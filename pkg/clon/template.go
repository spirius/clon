package clon

import (
	"bytes"
	"io/ioutil"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/juju/errors"

	"path/filepath"
)

func newTemplate(funcs map[string]interface{}) *template.Template {
	funcMap := sprig.TxtFuncMap()
	funcMap["file"] = tplFuncFile
	for name, fn := range funcs {
		funcMap[name] = fn
	}
	return template.New("").Funcs(funcMap)
}

func renderTemplate(content string, ctx interface{}, funcs map[string]interface{}) (_ string, resErr error) {
	var buf bytes.Buffer
	tpl, err := newTemplate(funcs).Parse(content)

	if err != nil {
		return "", errors.Trace(err)
	}

	defer func() {
		e := recover()
		if e != nil {
			switch err := e.(type) {
			case error:
				resErr = err
			default:
				panic(e)
			}
		} else {
			resErr = err
		}
	}()

	if err = tpl.Execute(&buf, ctx); err != nil {
		if tplErr, ok := err.(template.ExecError); ok {
			return "", errors.Wrapf(err, tplErr.Err, "error while executing template")
		}
		return buf.String(), errors.Trace(err)
	}

	return buf.String(), nil
}

func tplFuncFile(path string) (_ string, err error) {
	var content []byte
	p := filepath.Clean(path)

	if p[0] == '/' || p == "." {
		return "", errors.Errorf("Invalid path '%s', it is absolute or cannot be resolved", path)
	}

	if content, err = ioutil.ReadFile(p); err != nil {
		return "", errors.Trace(err)
	}

	return string(content), nil
}
