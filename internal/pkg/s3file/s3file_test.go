package s3file

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"testing"
	"time"

	"github.com/juju/errors"
	"github.com/stretchr/testify/require"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
)

var randSrc = rand.NewSource(time.Now().UnixNano())

type testReadCloser struct {
	bytes.Buffer
}

func (bc *testReadCloser) Close() error {
	return nil
}

type testCustomReadSeeker struct {
	r    *testReadSeeker
	read func(p []byte) (int, error)
	seek func(offset int64, whence int) (int64, error)
}

func (r *testCustomReadSeeker) Read(p []byte) (int, error) {
	if r.read == nil {
		return r.r.Read(p)
	}
	return r.read(p)
}

func (r *testCustomReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if r.seek == nil {
		return r.r.Seek(offset, whence)
	}
	return r.seek(offset, whence)
}

func newTestCustomReadSeeker(in []byte, read func(p []byte) (int, error), seek func(offset int64, whence int) (int64, error)) *testCustomReadSeeker {
	return &testCustomReadSeeker{
		r:    newTestReadSeeker(in),
		read: read,
		seek: seek,
	}
}

func newTestReadSeeker(in []byte) (r *testReadSeeker) {
	r = &testReadSeeker{data: make([]byte, len(in))}
	copy(r.data, in)
	return
}

type testReadSeeker struct {
	data []byte
	pos  int
}

func (r *testReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var p = 0
	var o = int(offset)
	switch whence {
	case io.SeekStart:
		p = o
	case io.SeekCurrent:
		p = r.pos + o
	case io.SeekEnd:
		p = len(r.data) + o
	default:
		return 0, fmt.Errorf("whence is wrong")
	}
	if p < 0 || p > len(r.data) {
		return 0, fmt.Errorf("seek out of bounds")
	}
	r.pos = p
	return int64(p), nil
}

func (r *testReadSeeker) Read(p []byte) (ret int, err error) {
	l := len(r.data) - r.pos
	n := len(p)
	if n >= l {
		ret = l
		err = io.EOF
	} else {
		ret = n
	}
	copy(p, r.data[r.pos:r.pos+ret])
	r.pos += ret
	return
}

func randomBytes(n int) (res []byte) {
	res = make([]byte, n)
	var (
		r, r2      int64
		left, need uint
		i          int
	)
	for i, r, left = 0, randSrc.Int63(), 63; i < n; i++ {
		if left < 8 {
			need = 8 - left
			r2 = randSrc.Int63()
			res[i] = byte((r << need) | (r2 & ((1 << need) - 1)))
			r = r2 >> need
			left = 63 - need
		} else {
			res[i] = byte(r)
			r = r >> 8
			left = left - 8
		}
	}
	return
}

type mockS3Client struct {
	s3iface.S3API
	putObject  func(*s3.PutObjectInput) (*s3.PutObjectOutput, error)
	headObject func(*s3.HeadObjectInput) (*s3.HeadObjectOutput, error)
	getObject  func(*s3.GetObjectInput) (*s3.GetObjectOutput, error)
}

func (m *mockS3Client) PutObject(in *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	return m.putObject(in)
}

func (m *mockS3Client) HeadObject(in *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	return m.headObject(in)
}

func (m *mockS3Client) GetObject(in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	return m.getObject(in)
}

func mockS3ClientPutObjectNoop(t *testing.T, versionID string) func(*s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	return func(in *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
		content, err := ioutil.ReadAll(in.Body)
		require.Nil(t, err)
		h := md5.Sum(content)

		if in.ContentMD5 != nil && aws.StringValue(in.ContentMD5) != base64.StdEncoding.EncodeToString(h[:]) {
			return nil, awserr.NewRequestFailure(awserr.New("InvalidDigest", `The Content-MD5 you specified was invalid.`, nil), 400, "")
		}
		out := &s3.PutObjectOutput{
			ETag: aws.String(fmt.Sprintf(`"%s"`, hex.EncodeToString(h[:]))),
		}

		if versionID != "" {
			out.VersionId = aws.String(versionID)
		}

		return out, nil
	}
}

func mockS3ClientHeadObjectError(t *testing.T, err error) func(*s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	return func(in *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
		return nil, err
	}
}

func mockS3ClientHeadObjectNoSuchKey(t *testing.T) func(*s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	return mockS3ClientHeadObjectError(t, awserr.NewRequestFailure(awserr.New(s3.ErrCodeNoSuchKey, `The specified key does not exist.`, nil), 404, "id"))
}

func mockS3ClientGetObjectError(t *testing.T, err error) func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	return func(in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		return nil, err
	}
}

func mockS3ClientGetObjectNoSuchKey(t *testing.T) func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	return mockS3ClientGetObjectError(t, awserr.NewRequestFailure(awserr.New(s3.ErrCodeNoSuchKey, `The specified key does not exist.`, nil), 404, "id"))
}

func TestSetLocation(t *testing.T) {
	var (
		require = require.New(t)
		bucket  = "mybucket"
		key     = "mykey"
		source  = "somesource.txt"
		prefix  = "test-prefix/"
		region  = "eu-central-1"
	)
	var inputs = []struct {
		config Config
		file   *File
		err    bool
	}{
		// no bucket
		{
			config: Config{},
			err:    true,
		},
		// no key
		{
			config: Config{Bucket: bucket},
			err:    true,
		},
		// basic
		{
			config: Config{Bucket: bucket, Key: key},
			err:    false,
			file:   &File{Bucket: bucket, Key: key},
		},
		// with region
		{
			config: Config{Bucket: bucket, Key: key, Region: region},
			err:    false,
			file:   &File{Bucket: bucket, Key: key, Region: region},
		},
		// with prefix and key
		{
			config: Config{Bucket: bucket, Key: key, Prefix: prefix},
			err:    false,
			file:   &File{Bucket: bucket, Key: prefix + key},
		},
		// with source
		{
			config: Config{Bucket: bucket, Source: source},
			err:    false,
			file:   &File{Bucket: bucket, Key: source},
		},
		// with prefix and source
		{
			config: Config{Bucket: bucket, Source: source, Prefix: prefix},
			err:    false,
			file:   &File{Bucket: bucket, Key: prefix + source},
		},
		// with prefix and source with sub-folder
		{
			config: Config{Bucket: bucket, Source: "asd/tet/123.txt", Prefix: prefix},
			err:    false,
			file:   &File{Bucket: bucket, Key: prefix + "123.txt"},
		},
	}
	for _, input := range inputs {
		file := &File{}
		err := file.setLocation(input.config)
		if input.err {
			require.NotNilf(err, "Input: %#+v, err: %s", input, input.err)
		} else {
			require.Equal(file, input.file)
		}
	}
}

func TestWrite_basic(t *testing.T) {
	var (
		require = require.New(t)
		region  = "eu-central-1"
		bucket  = "test-bucket"
	)
	var inputs = []struct {
		config Config
		err    bool
	}{
		// no Bucket
		{Config{
			Region: region,
		}, true},
		// no Content and Source
		{Config{
			Region: region,
			Bucket: bucket,
			Key:    "key",
		}, true},
		// Source not found
		{Config{
			Region: region,
			Bucket: bucket,
			Key:    "key",
			Source: "testdata/no-existing-file.txt",
		}, true},
		// file MaxSize check
		{Config{
			Region:  region,
			Bucket:  bucket,
			Key:     "key",
			MaxSize: 1,
			Source:  "testdata/s3file.txt",
		}, true},
		// MaxSize check
		{Config{
			Region:  region,
			Bucket:  bucket,
			Key:     "key",
			MaxSize: 10,
			Content: newTestReadSeeker(randomBytes(100)),
		}, true},
		// Content read error
		{Config{
			Region: region,
			Bucket: bucket,
			Key:    "key",
			Content: newTestCustomReadSeeker(
				nil,
				func(p []byte) (int, error) {
					return 0, fmt.Errorf("some error")
				},
				nil,
			),
		}, true},
		// Content seek error
		{Config{
			Region: region,
			Bucket: bucket,
			Key:    "key",
			Content: newTestCustomReadSeeker(
				nil,
				nil,
				func(offset int64, whence int) (int64, error) {
					return 0, fmt.Errorf("some error")
				},
			),
		}, true},

		// upload from Content
		{Config{
			Region:  region,
			Bucket:  bucket,
			Key:     "key",
			Content: newTestReadSeeker(randomBytes(100)),
		}, false},
		// upload from Source
		{Config{
			Region: region,
			Bucket: bucket,
			Key:    "from-source",
			Source: "testdata/s3file.txt",
		}, false},
		// upload 100 bytes
		{Config{
			Region:  region,
			Bucket:  bucket,
			Key:     "100-bytes",
			Content: newTestReadSeeker(randomBytes(100)),
		}, false},
		// upload 4096 bytes
		{Config{
			Region:  region,
			Bucket:  bucket,
			Key:     "4096-bytes",
			Content: newTestReadSeeker(randomBytes(4096)),
		}, false},
		// upload 5000 bytes
		{Config{
			Region:  region,
			Bucket:  bucket,
			Key:     "5000-bytes",
			Content: newTestReadSeeker(randomBytes(5000)),
		}, false},
		// with ContentType
		{Config{
			Region:      region,
			Bucket:      bucket,
			Key:         "5000-bytes",
			Content:     newTestReadSeeker(randomBytes(5000)),
			ContentType: "text/plain",
		}, false},
		// no region
		{Config{
			Bucket:      bucket,
			Key:         "5000-bytes",
			Content:     newTestReadSeeker(randomBytes(5000)),
			ContentType: "text/plain",
		}, false},
	}

	conn := &mockS3Client{
		headObject: mockS3ClientHeadObjectNoSuchKey(t),
		putObject:  mockS3ClientPutObjectNoop(t, ""),
	}

	for _, input := range inputs {
		config := input.config

		file, err := Write(conn, input.config)

		if input.err {
			require.Nilf(file, "file: %#+v", file)
			require.NotNil(err)
		} else {
			require.Nilf(err, "error: %s", err)
			require.NotNil(file)
			require.Equal(config.Bucket, file.Bucket)
			require.Equal(config.Region, file.Region)
			url := ""
			if file.Region != "" {
				url = "https://s3." + config.Region + ".amazonaws.com/" + config.Bucket + "/" + config.Key
			}
			require.Equal(url, file.URL)
			if config.ContentType == "" {
				require.Equal("application/octet-stream", file.ContentType)
			} else {
				require.Equal(config.ContentType, file.ContentType)
			}
		}
	}
}

func TestWrite_attributes(t *testing.T) {
	var (
		require   = require.New(t)
		region    = "eu-central-1"
		bucket    = "my-bucket"
		versionID = "123456789"
		key       = "my-key"
		content   = randomBytes(5000)
		hash      = md5.Sum(content)
		hashHex   = hex.EncodeToString(hash[:])
	)
	var inputs = []struct {
		config     Config
		headObject func(in *s3.HeadObjectInput) (*s3.HeadObjectOutput, error)
		putObject  func(in *s3.PutObjectInput) (*s3.PutObjectOutput, error)
		check      func(Config, *File, error)
	}{
		// File exists
		{
			config: Config{
				Region:  region,
				Bucket:  bucket,
				Key:     key,
				Content: newTestReadSeeker(content),
			},
			headObject: func(in *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
				return &s3.HeadObjectOutput{
					ETag:        aws.String(`"` + hashHex + `"`),
					VersionId:   aws.String(versionID),
					ContentType: aws.String("application/octet-stream"),
				}, nil
			},
			putObject: func(_ *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
				t.Fatalf("putObject should not be called")
				return nil, nil
			},
			check: func(config Config, file *File, err error) {
				require.Nil(err)
				require.NotNil(file)
				require.Equal(hashHex, file.Hash)
				require.Equal("https://s3."+region+".amazonaws.com/"+config.Bucket+"/"+config.Key+"?versionId="+versionID, file.URL)
			},
		},
		// HeadObject error
		{
			config: Config{
				Region:  region,
				Bucket:  bucket,
				Key:     key,
				Content: newTestReadSeeker(content),
			},
			headObject: func(in *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
				return nil, fmt.Errorf("some error")
			},
			putObject: func(_ *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
				t.Fatalf("putObject should not be called")
				return nil, nil
			},
			check: func(config Config, file *File, err error) {
				require.NotNil(err)
			},
		},
		// versionID
		{
			config: Config{
				Region:  region,
				Bucket:  bucket,
				Key:     key,
				Content: newTestReadSeeker(content),
			},
			headObject: mockS3ClientHeadObjectNoSuchKey(t),
			putObject:  mockS3ClientPutObjectNoop(t, versionID),
			check: func(config Config, file *File, err error) {
				require.Nil(err)
				require.NotNil(file)
				require.Equal(versionID, file.VersionID)
			},
		},
		// PutObject error
		{
			config: Config{
				Region:  region,
				Bucket:  bucket,
				Key:     key,
				Content: newTestReadSeeker(content),
			},
			headObject: mockS3ClientHeadObjectNoSuchKey(t),
			putObject: func(in *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
				return nil, fmt.Errorf("some error")
			},
			check: func(config Config, file *File, err error) {
				require.NotNil(err)
			},
		},
	}

	for _, input := range inputs {
		conn := &mockS3Client{
			headObject: input.headObject,
			putObject:  input.putObject,
		}
		file, err := Write(conn, input.config)
		input.check(input.config, file, err)
	}
}

func TestRead_basic(t *testing.T) {
	var (
		require   = require.New(t)
		region    = "eu-central-1"
		bucket    = "my-bucket"
		key       = "my-key"
		versionID = "123456789"
		content   = randomBytes(5000)
		hash      = md5.Sum(content)
		hashHex   = hex.EncodeToString(hash[:])
	)
	var inputs = []struct {
		config    Config
		getObject func(in *s3.GetObjectInput) (*s3.GetObjectOutput, error)
		check     func(Config, *File, error)
	}{
		// no bucket
		{
			config: Config{
				Key: key,
			},
			getObject: mockS3ClientGetObjectNoSuchKey(t),
			check: func(_ Config, _ *File, err error) {
				require.NotNil(err)
			},
		},
		// not found
		{
			config: Config{
				Bucket: bucket,
				Key:    key,
			},
			getObject: mockS3ClientGetObjectNoSuchKey(t),
			check: func(_ Config, _ *File, err error) {
				require.NotNil(err)
				require.True(errors.IsNotFound(err))
			},
		},
		// GetObject error
		{
			config: Config{
				Bucket: bucket,
				Key:    key,
			},
			getObject: mockS3ClientGetObjectError(t, fmt.Errorf("some error")),
			check: func(_ Config, _ *File, err error) {
				require.NotNil(err)
			},
		},

		// GetObject simple
		{
			config: Config{
				Bucket: bucket,
				Key:    key,
			},
			getObject: func(in *s3.GetObjectInput) (out *s3.GetObjectOutput, err error) {
				require.Equal(bucket, aws.StringValue(in.Bucket))
				require.Equal(key, aws.StringValue(in.Key))
				return &s3.GetObjectOutput{
					Body:          &testReadCloser{*bytes.NewBuffer(content)},
					ContentLength: aws.Int64(int64(len(content))),
					ETag:          aws.String(fmt.Sprintf(`"%s"`, hashHex)),
				}, nil
			},
			check: func(_ Config, file *File, err error) {
				require.Nil(err)
				require.NotNil(file)
				require.Equal(hashHex, file.Hash)
				require.Equal("", file.URL)
			},
		},
		// GetObject with region
		{
			config: Config{
				Region: region,
				Bucket: bucket,
				Key:    key,
			},
			getObject: func(in *s3.GetObjectInput) (out *s3.GetObjectOutput, err error) {
				require.Equal(bucket, aws.StringValue(in.Bucket))
				require.Equal(key, aws.StringValue(in.Key))
				return &s3.GetObjectOutput{
					Body:          &testReadCloser{*bytes.NewBuffer(content)},
					ContentLength: aws.Int64(int64(len(content))),
					ETag:          aws.String(fmt.Sprintf(`"%s"`, hashHex)),
				}, nil
			},
			check: func(_ Config, file *File, err error) {
				require.Nil(err)
				require.NotNil(file)
				require.Equal(hashHex, file.Hash)
				require.Equal("https://s3."+region+".amazonaws.com/"+bucket+"/"+key, file.URL)
			},
		},
		// GetObject version
		{
			config: Config{
				Region:    region,
				Bucket:    bucket,
				Key:       key,
				VersionID: versionID,
			},
			getObject: func(in *s3.GetObjectInput) (out *s3.GetObjectOutput, err error) {
				require.Equal(bucket, aws.StringValue(in.Bucket))
				require.Equal(key, aws.StringValue(in.Key))
				return &s3.GetObjectOutput{
					Body:          &testReadCloser{*bytes.NewBuffer(content)},
					ContentLength: aws.Int64(int64(len(content))),
					ETag:          aws.String(fmt.Sprintf(`"%s"`, hashHex)),
					VersionId:     aws.String(versionID),
				}, nil
			},
			check: func(_ Config, file *File, err error) {
				require.Nil(err)
				require.NotNil(file)
				require.Equal(hashHex, file.Hash)
				require.Equal("https://s3."+region+".amazonaws.com/"+bucket+"/"+key+"?versionId="+versionID, file.URL)
			},
		},
	}
	for _, input := range inputs {
		conn := &mockS3Client{
			getObject: input.getObject,
		}
		file, err := Read(conn, input.config)
		input.check(input.config, file, err)
	}
}
