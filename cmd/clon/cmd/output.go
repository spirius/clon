package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spirius/clon/pkg/cfn"
	"github.com/spirius/clon/pkg/clon"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/fatih/color"
	"github.com/juju/errors"
	log "github.com/sirupsen/logrus"
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

func outputStatus(s string) string {
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
	}
	return color.WhiteString(s)
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

func outputStack(w io.Writer, stack *clon.StackData, typ int) error {
	if typ == outputTypeStatusLine {
		log.Infof("stack status - %s [%s] %s",
			color.HiWhiteString(stack.Name),
			outputStatus(stack.Status),
			stack.StatusReason,
		)
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
	defer tw.Flush()
	fmt.Fprintf(tw, "%s:\t%s\n", color.HiWhiteString("Stack"), color.CyanString(stack.ConfigName))
	fmt.Fprintf(tw, "%s:\t%s\n", color.HiWhiteString("StackName"), stack.Name)
	fmt.Fprintf(tw, "%s:\t%s %s\n", color.HiWhiteString("StackStatus"), outputStatus(stack.Status), stack.StatusReason)
	if stack.Status != cfn.StackStatusNotFound {
		fmt.Fprintf(tw, "%s:\t%s\n", color.HiWhiteString("Id"), stack.ID)
	}
	if typ == outputTypeLong {
		if len(stack.Parameters) > 0 {
			fmt.Fprintf(tw, "%s:\n", color.HiWhiteString("Parameters"))
			for k, v := range stack.Parameters {
				fmt.Fprintf(tw, "  %s:\t%q\n", color.HiWhiteString(k), v)
			}
		}
		if len(stack.Outputs) > 0 {
			fmt.Fprintf(tw, "%s:\n", color.HiWhiteString("Outputs"))
			for k, v := range stack.Outputs {
				fmt.Fprintf(tw, "  %s:\t%q\n", color.HiWhiteString(k), v)
			}
		}
	}
	return nil
}

func outputPlanResourceChangeDetailsSingle(tw io.Writer, name string, details []*cloudformation.ResourceChangeDetail) {
	if name != "" {
		fmt.Fprintf(tw, "  %s:\n", color.HiWhiteString(name))
	}
	for _, d := range details {
		fmt.Fprintf(tw, "    %s: %s, ", color.HiWhiteString("ChangeSource"), aws.StringValue(d.ChangeSource))

		if d.CausingEntity != nil {
			fmt.Fprintf(tw, "%s: %s, ", color.HiWhiteString("CausingEntity"), aws.StringValue(d.CausingEntity))
		}
		fmt.Fprintf(tw, "%s: %s", color.HiWhiteString("Evaluation"), aws.StringValue(d.Evaluation))

		switch aws.StringValue(d.Target.RequiresRecreation) {
		case cloudformation.RequiresRecreationAlways:
			fmt.Fprintf(tw, " %s", color.RedString("(requires recreation)"))
		case cloudformation.RequiresRecreationConditionally:
			fmt.Fprintf(tw, " %s", color.HiRedString("(conditional recreation)"))
		default:
		}

		if configFlags.ignoreNestedUpdates && len(details) == 1 && aws.StringValue(d.ChangeSource) == "Automatic" &&
			aws.StringValue(d.Evaluation) == "Dynamic" &&
			aws.StringValue(d.Target.Attribute) == "Properties" &&
			aws.StringValue(d.Target.RequiresRecreation) == "Never" {
			fmt.Fprintf(tw, " %s", color.HiCyanString("(nested stack update check, ignored)"))
		}

		fmt.Fprintf(tw, "\n")
	}
}

func outputPlanResourceChangeDetails(tw io.Writer, details []*cloudformation.ResourceChangeDetail) {
	if len(details) == 0 {
		return
	}
	properties := make(map[string][]*cloudformation.ResourceChangeDetail)
	attributes := make(map[string][]*cloudformation.ResourceChangeDetail)
	pnames := make([]string, 0)
	anames := make([]string, 0)
	for _, detail := range details {
		if aws.StringValue(detail.Target.Attribute) == cloudformation.ResourceAttributeProperties {
			name := aws.StringValue(detail.Target.Name)
			p, ok := properties[name]
			if !ok {
				pnames = append(pnames, name)
			}
			properties[name] = append(p, detail)
		} else {
			name := aws.StringValue(detail.Target.Attribute)
			a, ok := attributes[name]
			if !ok {
				anames = append(anames, name)
			}
			attributes[name] = append(a, detail)
		}
	}

	if len(properties) > 0 {
		sort.Strings(pnames)
		for _, name := range pnames {
			_ = name
			outputPlanResourceChangeDetailsSingle(tw, name, properties[name])
		}
	}

	if len(attributes) > 0 {
		sort.Strings(anames)
		for _, name := range anames {
			_ = name
			outputPlanResourceChangeDetailsSingle(tw, name, attributes[name])
		}
	}
}

func outputPlan(w io.Writer, plan *clon.Plan, typ int) error {
	if typ == outputTypeShort {
		_, err := fmt.Fprintln(w, plan.ID)
		return errors.Trace(err)
	} else if typ != outputTypeLong {
		return errors.Errorf("output type %d for plan is not implemented", typ)
	}

	var cw = color.HiWhiteString
	var sv = aws.StringValue

	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
	defer tw.Flush()
	fmt.Fprintf(tw, "%s:\t%s\n", cw("Stack"), color.CyanString(plan.Stack.ConfigName))
	fmt.Fprintf(tw, "%s:\t%s\n", cw("StackName"), plan.Stack.Name)
	fmt.Fprintf(tw, "%s:\t%s\n", cw("StackStatus"), outputStatus(plan.Stack.Status))
	if plan.Stack.Status != cfn.StackStatusNotFound {
		fmt.Fprintf(tw, "%s:\t%s\n", cw("StackID"), plan.Stack.ID)
	}
	fmt.Fprintf(tw, "%s:\t%s\n", cw("ChageSetID"), plan.ChangeSet.ID)
	fmt.Fprintf(tw, "%s:\t%s\n", cw("ChangeSetName"), plan.ChangeSet.Name)
	fmt.Fprintf(tw, "%s:\t%s\n", cw("ExecutionStatus"), outputStatus(plan.ChangeSet.ExecutionStatus))
	fmt.Fprintf(tw, "%s:\t%s\n", cw("RoleARN"), plan.RoleARN.String())

	if plan.Parameters.HasChange() {
		fmt.Fprintf(tw, "\n%s:\n", cw("Parameters"))
		for name, param := range plan.Parameters {
			if param.IsEqual() {
				continue
			}
			fmt.Fprintf(tw, "  %s:\t%s\n", cw(name), color.YellowString(param.String()))
		}
	}

	if len(plan.ChangeSet.Changes) > 0 {
		fmt.Fprintf(tw, "\n%s:\n", cw("ResourceChanges"))
		for _, res := range plan.ChangeSet.Changes {
			var col color.Attribute
			var sign byte
			switch sv(res.Action) {
			case cloudformation.ChangeActionAdd:
				col = color.FgGreen
				sign = '+'
			case cloudformation.ChangeActionRemove:
				col = color.FgRed
				sign = '-'
			case cloudformation.ChangeActionModify:
				switch sv(res.Replacement) {
				case cloudformation.ReplacementTrue:
					col = color.FgRed
					sign = '±'
				case cloudformation.ReplacementFalse:
					col = color.FgYellow
					sign = '~'
				case cloudformation.ReplacementConditional:
					col = color.FgHiRed
					sign = '?'
				}
			}
			fmt.Fprintf(tw, "%s (%s)\n", color.New(col).Sprintf("[%c] %s", sign, sv(res.LogicalResourceId)), sv(res.ResourceType))
			outputPlanResourceChangeDetails(tw, res.Details)
			fmt.Fprintf(tw, "\n")
		}
	}

	return nil
}

func outputChangeSet(_ io.Writer, cs *cfn.ChangeSetData, typ int) error {
	if typ != outputTypeStatusLine {
		return errors.Errorf("output type %d for change set is not implemented", typ)
	}
	log.Infof("changeset status - %s [%s] %s",
		color.HiWhiteString(cs.Name),
		outputStatus(cs.Status),
		cs.StatusReason,
	)
	return nil
}

func outputStackEvent(_ io.Writer, cs *cfn.StackEventData, typ int) error {
	if typ != outputTypeStatusLine {
		return errors.Errorf("output type %d for change set is not implemented", typ)
	}
	log.Infof("resource status - %s:%s (%s) - [%s] %s",
		color.HiWhiteString(cs.StackName),
		color.HiWhiteString(cs.LogicalResourceID),
		cs.ResourceType,
		outputStatus(cs.ResourceStatus),
		cs.ResourceStatusReason,
	)
	return nil
}
