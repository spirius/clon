package cmd

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/fatih/color"
	"github.com/juju/errors"
	"github.com/spirius/clon/internal/pkg/cfn"
)

func addColorFunc(funcMap map[string]interface{}, name string, fn func(format string, a ...interface{}) string) {
	funcMap[name] = func(content string) string {
		return fn("%s", content)
	}
	funcMap[name+"f"] = func(format string, a ...interface{}) string {
		return fn(format, a...)
	}
}

var tplHandler *template.Template

func init() {
	funcMap := sprig.TxtFuncMap()

	addColorFunc(funcMap, "black", color.BlackString)
	addColorFunc(funcMap, "blue", color.BlueString)
	addColorFunc(funcMap, "cyan", color.CyanString)
	addColorFunc(funcMap, "green", color.GreenString)
	addColorFunc(funcMap, "hiblack", color.HiBlackString)
	addColorFunc(funcMap, "hiblue", color.HiBlueString)
	addColorFunc(funcMap, "hicyan", color.HiCyanString)
	addColorFunc(funcMap, "higreen", color.HiGreenString)
	addColorFunc(funcMap, "himagenta", color.HiMagentaString)
	addColorFunc(funcMap, "hired", color.HiRedString)
	addColorFunc(funcMap, "hiwhite", color.HiWhiteString)
	addColorFunc(funcMap, "hiyellow", color.HiYellowString)
	addColorFunc(funcMap, "hiwhite", color.HiWhiteString)
	addColorFunc(funcMap, "magenta", color.MagentaString)
	addColorFunc(funcMap, "red", color.RedString)
	addColorFunc(funcMap, "white", color.WhiteString)
	addColorFunc(funcMap, "yellow", color.YellowString)

	funcMap["status"] = func(s string) string {
		if s == cloudformation.StackStatusRollbackInProgress {
			return color.HiRedString(s)
		} else if s == cloudformation.StackStatusRollbackComplete {
			return color.RedString(s)
		} else if strings.HasSuffix(s, "_COMPLETE") || s == "AVAILABLE" {
			return color.GreenString(s)
		} else if strings.HasSuffix(s, "_IN_PROGRESS") || strings.HasSuffix(s, "_PENDING") {
			return color.YellowString(s)
		} else if s == cfn.StackStatusNotFound || s == "UNAVAILABLE" {
			return color.HiBlackString(s)
		} else if strings.HasSuffix(s, "_FAILED") {
			return color.RedString(s)
		} else {
			return color.WhiteString(s)
		}
	}

	funcMap["weq"] = func(a *string, b string) bool {
		if a == nil {
			return false
		}
		return *a == b
	}

	funcMap["awsStringValue"] = func(a *string) string {
		return aws.StringValue(a)
	}

	tplHandler = template.New("").Funcs(funcMap)
}

func render(w io.Writer, content, defs string, ctx interface{}) error {
	content = fmt.Sprintf(`%s{{ with $.ctx }}%s{{ end }}`, defs, content)
	tpl, err := tplHandler.Parse(content)
	if err != nil {
		return errors.Annotatef(err, "cannot parse template")
	}
	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
	newCtx := map[string]interface{}{
		"ctx": ctx,
		"const": map[string]string{
			"ChangeActionAdd":    cloudformation.ChangeActionAdd,
			"ChangeActionModify": cloudformation.ChangeActionModify,
			"ChangeActionRemove": cloudformation.ChangeActionRemove,

			"ReplacementTrue":        cloudformation.ReplacementTrue,
			"ReplacementFalse":       cloudformation.ReplacementFalse,
			"ReplacementConditional": cloudformation.ReplacementConditional,

			"ResourceAttributeProperties":     cloudformation.ResourceAttributeProperties,
			"ResourceAttributeMetadata":       cloudformation.ResourceAttributeMetadata,
			"ResourceAttributeCreationPolicy": cloudformation.ResourceAttributeCreationPolicy,
			"ResourceAttributeUpdatePolicy":   cloudformation.ResourceAttributeUpdatePolicy,
			"ResourceAttributeDeletionPolicy": cloudformation.ResourceAttributeDeletionPolicy,
			"ResourceAttributeTags":           cloudformation.ResourceAttributeTags,

			"RequiresRecreationNever":         cloudformation.RequiresRecreationNever,
			"RequiresRecreationConditionally": cloudformation.RequiresRecreationConditionally,
			"RequiresRecreationAlways":        cloudformation.RequiresRecreationAlways,
		},
	}
	if err = tpl.Execute(tw, newCtx); err != nil {
		return errors.Annotatef(err, "cannot execute template")
	}
	if err = tw.Flush(); err != nil {
		return errors.Trace(err)
	}
	return nil
}
