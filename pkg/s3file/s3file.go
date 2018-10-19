package s3file

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/juju/errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
)

// Config represents the input configuration for
// file S3 file Read and Write functions.
type Config struct {
	// Bucket is the bucket name.
	Bucket string

	// Key is the key in s3 bucket.
	// One of Key or Source must be specified
	Key string

	// Source is the local file path. Used only by Write.
	Source string

	// Prefix is added to the Key attribute and used as final S3
	// bucket key.
	Prefix string

	// Content of the file. Used only by Write.
	Content io.ReadSeeker

	// ContentType is the content-type of file.
	ContentType string

	// MaxSize is the maximum size of Read or Write operation.
	MaxSize int64

	// VersionID is the version of S3 object. Used only
	// by Read.
	VersionID string

	// Region is the AWS region of the bucket.
	// File URL is constructed only if Region sepcified.
	Region string
}

// File represents S3 Object
type File struct {
	// Bucket is the bucket of file.
	Bucket string

	// Key is the key of object in bucket.
	Key string

	// VersionID is the version of s3 object.
	VersionID string

	// Hash is the md5 of content in hex representation.
	Hash string

	// Body is the content of object. Set by Read function.
	Body io.ReadCloser

	// ContentType is the content-type header of s3 object.
	ContentType string

	// Region is the region of bucket.
	Region string

	// URL is the https URL of the file.
	// Example: https://s3.eu-central-1.amazonaws.com/mybucket/mykey
	URL string
}

func setHash(r io.ReadSeeker, h hash.Hash, maxSize int64) (err error) {
	buf := make([]byte, 4096)
	size := int64(0)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			size += int64(n)
			if maxSize != 0 && size > maxSize {
				return errors.Errorf("file is too big, size is > %d", maxSize)
			}
			h.Write(buf[:n])
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Annotatef(err, "hash calculation failed, cannot read")
		}
	}
	if _, err = r.Seek(0, io.SeekStart); err != nil {
		return errors.Annotatef(err, "hash calculation failed, cannot seek")
	}
	return nil
}

func (f *File) setLocation(c Config) error {
	if c.Bucket == "" {
		return errors.Errorf("bucket is not set")
	}
	f.Bucket = c.Bucket

	// identify key
	if c.Prefix != "" {
		f.Key = c.Prefix
	}
	if c.Key == "" {
		if c.Source == "" {
			return errors.Errorf("neither key nor source are set")
		}
		f.Key += filepath.Base(c.Source)
	} else {
		f.Key += c.Key
	}

	f.Region = c.Region

	return nil
}

func (f *File) setURL() {
	if f.Region == "" {
		return
	}
	f.URL = fmt.Sprintf(
		"https://s3.%s.amazonaws.com/%s/%s",
		f.Region,
		f.Bucket,
		f.Key)
	if f.VersionID != "" {
		f.URL += fmt.Sprintf("?versionId=%s", url.PathEscape(f.VersionID))
	}
}

// Write writes an S3 object. If file exists and hash not changed,
// returns VersionID of existing file.
func Write(conn s3iface.S3API, c Config) (*File, error) {
	var (
		content io.ReadSeeker
		f       = &File{}
		err     error
	)

	if err = f.setLocation(c); err != nil {
		return nil, errors.Annotatef(err, "s3 write failed")
	}

	// identify content
	if c.Content == nil && c.Source == "" {
		return nil, errors.Errorf("s3 upload failed, neither content nor source are set")
	} else if c.Content == nil {
		content, err = os.Open(c.Source)
		if err != nil {
			return nil, errors.Annotatef(err, "s3 upload failed, cannot open file")
		}
	} else {
		content = c.Content
	}

	// calculate content hash
	hash := md5.New()
	if err = setHash(content, hash, c.MaxSize); err != nil {
		return nil, errors.Annotatef(err, "s3 upload failed")
	}
	h := hash.Sum(nil)
	h = h[:]
	f.Hash = hex.EncodeToString(h)

	if c.ContentType == "" {
		f.ContentType = "application/octet-stream"
	} else {
		f.ContentType = c.ContentType
	}

	// check if file already exists
	prev, err := conn.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(f.Bucket),
		Key:    aws.String(f.Key),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.RequestFailure); !ok || awsErr.StatusCode() != 404 {
			return nil, errors.Annotatef(err, "s3 upload failed, cannot read previous file")
		}
	} else if aws.StringValue(prev.ContentType) == f.ContentType && aws.StringValue(prev.ETag) == fmt.Sprintf(`"%s"`, f.Hash) {
		// file is not changed
		f.VersionID = aws.StringValue(prev.VersionId)
		f.setURL()
		return f, err
	}

	out, err := conn.PutObject(&s3.PutObjectInput{
		Bucket:      aws.String(f.Bucket),
		Key:         aws.String(f.Key),
		ContentType: aws.String(f.ContentType),
		ContentMD5:  aws.String(base64.StdEncoding.EncodeToString(h)),
		Body:        content,
	})
	if err != nil {
		return nil, errors.Annotatef(err, "s3 upload failed")
	}
	f.VersionID = aws.StringValue(out.VersionId)
	f.setURL()
	return f, err
}

// Read will read file specified by Config from S3.
func Read(conn s3iface.S3API, c Config) (*File, error) {
	var (
		f   = &File{}
		err error
	)
	if err = f.setLocation(c); err != nil {
		return nil, errors.Annotatef(err, "s3 read failed")
	}

	in := &s3.GetObjectInput{
		Bucket: aws.String(f.Bucket),
		Key:    aws.String(f.Key),
	}

	if c.VersionID != "" {
		in.VersionId = aws.String(c.VersionID)
	}

	out, err := conn.GetObject(in)
	if err != nil {
		if awsErr, ok := err.(awserr.RequestFailure); !ok || awsErr.Code() != s3.ErrCodeNoSuchKey {
			return nil, errors.Annotatef(err, "s3 read failed")
		}
		// file not found
		return nil, errors.NotFoundf("file '%s/%s' not found", f.Bucket, f.Key)
	}

	f.VersionID = aws.StringValue(out.VersionId)
	h := aws.StringValue(out.ETag)
	if len(h) > 2 {
		// remove quotes
		f.Hash = h[1 : len(h)-1]
	}
	f.Body = out.Body
	f.setURL()
	return f, nil
}
