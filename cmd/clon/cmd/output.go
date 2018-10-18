package cmd

import (
	"fmt"
	"io"

	"github.com/spirius/clon/internal/pkg/cfn"
	"github.com/spirius/clon/pkg/clon"

	"github.com/juju/errors"
)

func newOutput(in interface{}) output {
	return &outputCommon{in, outputTypeLong}
}

const (
	outputTypeLong = iota
	outputTypeShort
	outputTypeStatusLine
)

type output interface {
	Long() output
	Short() output
	StatusLine() output

	Output(io.Writer)
}

type outputCommon struct {
	data interface{}
	typ  int
}

func (o *outputCommon) Short() output {
	return &outputCommon{o.data, outputTypeShort}
}

func (o *outputCommon) Long() output {
	return &outputCommon{o.data, outputTypeLong}
}

func (o *outputCommon) StatusLine() output {
	return &outputCommon{o.data, outputTypeStatusLine}
}

func (o *outputCommon) Output(w io.Writer) {
	var err error
	switch data := o.data.(type) {
	case *clon.StackData:
		err = outputStack(w, data, o.typ)
	case *cfn.ChangeSetData:
		err = outputChangeSet(w, data, o.typ)
	case *cfn.StackEventData:
		err = outputStackEvent(w, data, o.typ)
	case *clon.Plan:
		err = outputPlan(w, data, o.typ)
	default:
		err = errors.Errorf("unknown data: %#+v", o.data)
	}
	if err != nil {
		panic(err)
	}
}

const outputStackShortTpl = `
{{- "Stack" | hiwhite }}:	{{ .ConfigName | cyan }}
{{ "StackName" | hiwhite }}:	{{ .Name }}
{{ "StackStatus" | hiwhite }}:	{{ .Status | status }} {{ .StatusReason }}
{{- if ne .Status "STACK_NOT_FOUND" }}
{{ "Id" | hiwhite}}:	{{ .ID }}
{{- end }}
`

const outputStackLongTpl = outputStackShortTpl +
	`{{ "Parameters" | hiwhite }}:
{{- range $k, $v := .Parameters }}
  {{ $k | hiwhite }}:	{{ $v | quote }}
{{- end }}
{{ "Outputs" | hiwhite }}:
{{- range $k, $v := .Outputs }}
  {{ $k | hiwhite }}:	{{ $v | quote }}
{{- end }}
`

const outputStackStatusLineTpl = `{{ "info" | cyan }}: {{ .Name }} - {{ .Status | status }}{{ if .StatusReason }} - {{ .StatusReason }}{{ end }}
`

const outputChangeSetStatusLineTpl = `{{ "info" | cyan }}: change set {{ .Name }} - {{ .Status | status }}{{ if .StatusReason }} - {{ .StatusReason }}{{ end }}
`

const outputStackEventStatusLineTpl = `{{ "info" | cyan }}: {{ .StackName }}, {{ .LogicalResourceID | hiwhite }} ({{ .ResourceType }}) - ` +
	`{{ .ResourceStatus | status }}{{ if .ResourceStatusReason }} - {{ .ResourceStatusReason }}{{ end }}
`

func outputStack(w io.Writer, stack *clon.StackData, typ int) error {
	var tpl string
	switch typ {
	case outputTypeShort:
		tpl = outputStackShortTpl
	case outputTypeLong:
		tpl = outputStackLongTpl
	case outputTypeStatusLine:
		tpl = outputStackStatusLineTpl
	default:
		return errors.Errorf("output type %d for stack is not implemented", typ)
	}
	return errors.Trace(render(w, tpl, "", stack))
}

const outputPlanLongTplDefs = `
{{- define "changeDetails" }}
    {{- $.Name | hiwhite }}:
{{- range $_, $d := .Details }}
      {{ "CausedBy" | hiwhite}}: {{ $d.ChangeSource }}
      {{- if $d.CausingEntity }}, {{ "CausingEntity" | hiwhite}}: {{ $d.CausingEntity }}{{ end }}
      {{- ", " }}{{ "Evaluation" | hiwhite }}: {{ $d.Evaluation }}
    {{- if weq $d.Target.RequiresRecreation $.const.RequiresRecreationAlways }}
      {{- red " (forces recreation)" }}
    {{- else if weq $d.Target.RequiresRecreation $.const.RequiresRecreationConditionally }}
      {{- hired " (conditional recreation)" }}
    {{- end -}}
{{ end }}
{{- end -}}{{/* end define changeDetails */}}

{{- define "changes" -}}
{{- $properties := dict -}}
{{- $attributes := dict -}}

{{- range $_, $d := .Details -}}
  {{- if weq $d.Target.Attribute $.const.ResourceAttributeProperties -}}
    {{- $name := $d.Target.Name | awsStringValue }}
    {{- $_ := set $properties $name (append (default list (index $properties $name)) $d) -}}
  {{- else -}}
    {{- $name := $d.Target.Attribute | awsStringValue }}
    {{- $_ := set $attributes $name (append (default list (index $attributes $name)) $d) -}}
  {{- end -}}
{{- end -}}
{{- if $properties -}}
{{- range $name, $d := $properties -}}
{{ "    " }}{{ template "changeDetails" (merge (dict "Details" $d "Name" $name) (omit $ "Details" "Name")) }}
{{ end }}
{{ end }}

{{- if $attributes -}}
{{- range $name, $d := $attributes -}}
{{ "    " }}{{ template "changeDetails" (merge (dict "Details" $d "Name" $name) (omit $ "Details" "Name")) }}
{{- end }}
{{ end }}{{/* end if $attributes */}}

{{- end -}}
`

const outputPlanLongTpl = `
{{- "Stack" | hiwhite }}:	{{ .Stack.ConfigName | cyan }}
{{ "StackName" | hiwhite }}:	{{ .Stack.Name }}
{{ "StackStatus" | hiwhite }}:	{{ .Stack.Status | status }} {{ .Stack.StatusReason }}
{{- if ne .Stack.Status "STACK_NOT_FOUND" }}
{{ "Id" | hiwhite}}:	{{ .Stack.ID }}
{{- end }}
{{ "ChangeSetId" | hiwhite }}:	{{ .ChangeSet.ID }}
{{ "ChangeSetName" | hiwhite }}:	{{ .ChangeSet.Name }}
{{ "ExecutionStatus" | hiwhite }}:	{{ .ChangeSet.ExecutionStatus | status }}
{{ "RoleARN" | hiwhite }}:	{{ .RoleARN.String }}

{{- if .Parameters.HasChange }}

{{ "Parameters" | hiwhite }}:
{{- range $name, $d := .Parameters }}
  {{- if not $d.IsEqual }}
  {{ $name | hiwhite }}:	{{ $d.String | yellow }}
  {{- end }}
{{- end }}
{{- end }}

{{- if .ChangeSet.Changes }}

{{ "ResourceChanges" | hiwhite }}:
{{- range $name, $r := .ChangeSet.Changes }}
  {{- if weq $r.Action $.const.ChangeActionAdd }}
{{ greenf "[+] "}}{{ $r.LogicalResourceId | green }} ({{ $r.ResourceType }})
  {{- else if weq $r.Action $.const.ChangeActionRemove }}
{{ redf "[-] "}}{{ $r.LogicalResourceId | red }} ({{ $r.ResourceType }})
  {{- else if weq $r.Action $.const.ChangeActionModify }}
    {{- if weq $r.Replacement $.const.ReplacementTrue }}
{{ redf "[±] "}}{{ $r.LogicalResourceId | red }} ({{ $r.ResourceType }})
    {{- else if weq $r.Replacement $.const.ReplacementFalse }}
{{ yellowf "[~] "}}{{ $r.LogicalResourceId | yellow }} ({{ $r.ResourceType }})
    {{- else }}
{{ hiredf "[?] "}}{{ $r.LogicalResourceId | hired }} ({{ $r.ResourceType }})
    {{- end }}
{{ template "changes" (merge (dict "Details" $r.Details) $) }}
  {{- end }}
{{- end }}
{{- end }}

`

func outputPlan(w io.Writer, plan *clon.Plan, typ int) error {
	if typ == outputTypeLong {
		return errors.Trace(render(w, outputPlanLongTpl, outputPlanLongTplDefs, plan))
	} else if typ == outputTypeShort {
		_, err := fmt.Fprintln(w, plan.ID)
		return errors.Trace(err)
	}
	return errors.Errorf("output type %d for plan is not implemented", typ)
}

func outputChangeSet(w io.Writer, cs *cfn.ChangeSetData, typ int) error {
	if typ != outputTypeStatusLine {
		return errors.Errorf("output type %d for change set is not implemented", typ)
	}
	return errors.Trace(render(w, outputChangeSetStatusLineTpl, "", cs))
}

func outputStackEvent(w io.Writer, cs *cfn.StackEventData, typ int) error {
	if typ != outputTypeStatusLine {
		return errors.Errorf("output type %d for change set is not implemented", typ)
	}
	return errors.Trace(render(w, outputStackEventStatusLineTpl, "", cs))
}
