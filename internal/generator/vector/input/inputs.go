package input

import (
	"fmt"
	. "github.com/openshift/cluster-logging-operator/internal/generator/framework"
	helpers2 "github.com/openshift/cluster-logging-operator/internal/generator/utils"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/source"
	"sort"
	"strings"

	"github.com/openshift/cluster-logging-operator/internal/generator/vector/normalize"

	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	. "github.com/openshift/cluster-logging-operator/internal/generator/vector/elements"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/helpers"
)

const (
	NsKube      = "kube"
	NsOpenshift = "openshift"
	NsDefault   = "default"

	K8sNamespaceName = ".kubernetes.namespace_name"
	K8sLabelKeyExpr  = ".kubernetes.labels.%q"

	InputContainerLogs = "container_logs"
	InputJournalLogs   = "journal_logs"

	RouteApplicationLogs = "route_application_logs"

	SrcPassThrough = "."

	UserDefinedSourceThrottle = `source_throttle_%s`
	perContainerLimitKeyField = `"{{ file }}"`
)

var (
	InfraContainerLogs = helpers.OR(
		helpers.StartWith(K8sNamespaceName, NsKube+"-"),
		helpers.StartWith(K8sNamespaceName, NsOpenshift+"-"),
		helpers.Eq(K8sNamespaceName, NsDefault),
		helpers.Eq(K8sNamespaceName, NsOpenshift),
		helpers.Eq(K8sNamespaceName, NsKube))
	AppContainerLogs = helpers.Neg(helpers.Paren(InfraContainerLogs))

	AddLogTypeApp   = fmt.Sprintf(".log_type = %q", logging.InputNameApplication)
	AddLogTypeInfra = fmt.Sprintf(".log_type = %q", logging.InputNameInfrastructure)
	AddLogTypeAudit = fmt.Sprintf(".log_type = %q", logging.InputNameAudit)

	UserDefinedInput = fmt.Sprintf("%s.%%s", RouteApplicationLogs)

	MatchNS = func(ns string) string {
		return helpers.Eq(K8sNamespaceName, ns)
	}
	K8sLabelKey = func(k string) string {
		return fmt.Sprintf(K8sLabelKeyExpr, k)
	}
	MatchLabel = func(k, v string) string {
		return helpers.Eq(K8sLabelKey(k), v)
	}
)

func AddThrottle(spec *logging.InputSpec) []Element {
	var (
		threshold    int64
		throttle_key string
	)

	el := []Element{}
	input := fmt.Sprintf(UserDefinedInput, spec.Name)

	if spec.Application.ContainerLimit != nil {
		threshold = spec.Application.ContainerLimit.MaxRecordsPerSecond
		throttle_key = perContainerLimitKeyField
	} else {
		threshold = spec.Application.GroupLimit.MaxRecordsPerSecond
	}

	el = append(el, normalize.NewThrottle(
		fmt.Sprintf(UserDefinedSourceThrottle, spec.Name),
		[]string{input},
		threshold,
		throttle_key,
	)...)

	return el
}

// Inputs takes the raw log sources (container, journal, audit) and produces Inputs as defined by ClusterLogForwarder Api
func Inputs(spec *logging.ClusterLogForwarderSpec, o Options) []Element {
	el := []Element{}

	types := helpers2.GatherSources(spec, o)
	// route container_logs based on type
	if types.Has(logging.InputNameApplication) || types.Has(logging.InputNameInfrastructure) {
		r := Route{
			ComponentID: "route_container_logs",
			Inputs:      helpers.MakeInputs(InputContainerLogs),
			Routes:      map[string]string{},
		}
		if types.Has(logging.InputNameApplication) {
			r.Routes["app"] = helpers.Quote(AppContainerLogs)
		}
		if types.Has(logging.InputNameInfrastructure) {
			r.Routes["infra"] = helpers.Quote(InfraContainerLogs)
		}
		el = append(el, r)
	}

	if types.Has(logging.InputNameApplication) {
		el = append(el, Remap{
			Desc:        `Set log_type to "application"`,
			ComponentID: logging.InputNameApplication,
			Inputs:      helpers.MakeInputs("route_container_logs.app"),
			VRL:         AddLogTypeApp,
		})
	}
	if types.Has(logging.InputNameInfrastructure) {
		el = append(el, Remap{
			Desc:        `Set log_type to "infrastructure"`,
			ComponentID: logging.InputNameInfrastructure,
			Inputs:      helpers.MakeInputs("route_container_logs.infra", InputJournalLogs),
			VRL:         AddLogTypeInfra,
		})
	}
	if types.Has(logging.InputNameAudit) {
		el = append(el,
			Remap{
				Desc:        `Set log_type to "audit"`,
				ComponentID: logging.InputNameAudit,
				Inputs:      helpers.MakeInputs(source.HostAuditLogs, source.K8sAuditLogs, source.OpenshiftAuditLogs, source.OvnAuditLogs),
				VRL: strings.Join(helpers.TrimSpaces([]string{
					AddLogTypeAudit,
					normalize.FixHostname,
					normalize.FixTimestampField,
				}), "\n"),
			})
	}

	userDefinedAppRouteMap := UserDefinedAppRouting(spec, o)
	if len(userDefinedAppRouteMap) != 0 {
		var keys []string
		for key := range userDefinedAppRouteMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		el = append(el, Route{
			ComponentID: RouteApplicationLogs,
			Inputs:      helpers.MakeInputs(logging.InputNameApplication),
			Routes:      userDefinedAppRouteMap,
		})

		userDefined := spec.InputMap()
		for _, inRef := range keys {
			if input, ok := userDefined[inRef]; ok && input.HasPolicy() && input.GetMaxRecordsPerSecond() > 0 {
				// Vector Throttle component cannot have zero threshold
				el = append(el, AddThrottle(input)...)
			}
		}
	}

	for _, input := range spec.Inputs {
		if logging.IsAuditHttpReceiver(&input) {
			el = append(el,
				Remap{
					Desc:        `Set log_type to "audit"`,
					ComponentID: input.Name + `_input`,
					Inputs:      helpers.MakeInputs(input.Name + `_normalized`),
					VRL: strings.Join(helpers.TrimSpaces([]string{
						AddLogTypeAudit,
						normalize.FixHostname,
						normalize.FixTimestampField,
					}), "\n"),
				})
		}
		if logging.IsSyslogReceiver(&input) {
			el = append(el,
				Remap{
					Desc:        `Set log_type to "infrastructure"`,
					ComponentID: `syslog_input`,
					// Feeding the raw, untransformed openstack logs works well
					Inputs: helpers.MakeInputs(`raw_syslog_logs`),
					VRL:    AddLogTypeInfra,
				})
		}
	}

	return el
}

func UserDefinedAppRouting(spec *logging.ClusterLogForwarderSpec, o Options) map[string]string {
	userDefined := spec.InputMap()
	routeMap := map[string]string{}
	for _, pipeline := range spec.Pipelines {
		for _, inRef := range pipeline.InputRefs {
			if input, ok := userDefined[inRef]; ok {
				// user defined input
				if input.Application != nil {
					app := input.Application
					matchNS := []string{}
					if len(app.Namespaces) != 0 {
						for _, ns := range app.Namespaces {
							matchNS = append(matchNS, MatchNS(ns))
						}
					}
					matchLabels := []string{}
					if app.Selector != nil && len(app.Selector.MatchLabels) != 0 {
						labels := app.Selector.MatchLabels
						keys := make([]string, 0, len(labels))
						for k := range labels {
							keys = append(keys, k)
						}
						sort.Strings(keys)
						for _, k := range keys {
							matchLabels = append(matchLabels, MatchLabel(k, labels[k]))
						}
					}
					if len(matchNS) != 0 || len(matchLabels) != 0 {
						routeMap[input.Name] = helpers.Quote(helpers.AND(helpers.OR(matchNS...), helpers.AND(matchLabels...)))
					} else if input.HasPolicy() {
						routeMap[input.Name] = "'true'"
					}
				}
			}
		}
	}
	return routeMap
}
