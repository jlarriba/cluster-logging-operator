package input_selection

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	framework "github.com/openshift/cluster-logging-operator/test/framework/e2e"
	"github.com/openshift/cluster-logging-operator/test/framework/functional"
	"github.com/openshift/cluster-logging-operator/test/helpers"
	testruntime "github.com/openshift/cluster-logging-operator/test/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These tests exist as e2e because vector interacts directly with the API server
// and various bits of functionality are not testable using the functional
// framework
var _ = Describe("[InputSelection]", func() {

	const (
		valueBackend  = "backend"
		valueFrontend = "frontend"
		valueMiddle   = "middle"
		component     = "component"
	)

	var (
		e2e      *framework.E2ETestFramework
		receiver *framework.VectorHttpReceiverLogStore
		err      error

		logGeneratorNameFn = func(name string) string {
			return "log-generator"
		}
	)

	AfterEach(func() {
		if e2e != nil {
			e2e.Cleanup()
		}
	})

	var _ = DescribeTable("filtering", func(input logging.InputSpec, generatorName func(string) string, verify func()) {
		e2e = framework.NewE2ETestFramework()
		forwarder := testruntime.NewClusterLogForwarder()
		forwarder.Namespace = e2e.CreateTestNamespace()
		forwarder.Name = "my-log-collector"
		if generatorName == nil {
			generatorName = func(component string) string {
				return component
			}
		}

		for componentName, namespace := range map[string]string{
			valueFrontend: e2e.CreateTestNamespace(),
			valueBackend:  e2e.CreateTestNamespace(),
			valueMiddle:   e2e.CreateTestNamespaceWithPrefix("openshift-test")} {
			if err := e2e.DeployLogGeneratorWithNamespaceAndLabels(namespace, generatorName(componentName), map[string]string{
				"testtype": "myinfra",
				component:  componentName,
			}); err != nil {
				Fail(fmt.Sprintf("Timed out waiting for the log generator to deploy: %v", err))
			}
		}

		receiver, err = e2e.DeployHttpReceiver(forwarder.Namespace)
		Expect(err).To(BeNil())
		sa, err := e2e.BuildAuthorizationFor(forwarder.Namespace, forwarder.Name).
			AllowClusterRole("collect-application-logs").
			AllowClusterRole("collect-infrastructure-logs").
			AllowClusterRole("collect-audit-logs").
			Create()
		Expect(err).To(BeNil())
		forwarder.Spec.ServiceAccountName = sa.Name
		functional.NewClusterLogForwarderBuilder(forwarder).
			FromInputWithVisitor("myinput", func(spec *logging.InputSpec) {
				spec.Application = input.Application
				spec.Infrastructure = input.Infrastructure
				spec.Audit = input.Audit
			}).ToOutputWithVisitor(func(spec *logging.OutputSpec) {
			spec.Type = logging.OutputTypeHttp
			spec.URL = receiver.ClusterLocalEndpoint()
		}, "my-output")
		if err := e2e.CreateClusterLogForwarder(forwarder); err != nil {
			Fail(fmt.Sprintf("Unable to create an instance of logforwarder: %v", err))
		}
		components := []helpers.LogComponentType{helpers.ComponentTypeCollector}
		for _, component := range components {
			if err := e2e.WaitForDaemonSet(forwarder.Namespace, forwarder.Name); err != nil {
				Fail(fmt.Sprintf("Failed waiting for component %s to be ready: %v", component, err))
			}
		}
		verify()
	},
		Entry("infrastructure inputs should allow specifying only node logs",
			logging.InputSpec{
				Infrastructure: &logging.Infrastructure{
					Sources: []string{logging.InfrastructureSourceNode},
				},
			},
			nil,
			func() {
				Expect(receiver.ListJournalLogs()).ToNot(HaveLen(0), "exp only journal logs to be collected")
				Expect(receiver.ListNamespaces()).To(HaveLen(0), "exp no containers logs to be collected")
			}),
		Entry("infrastructure inputs should allow specifying only container logs",
			logging.InputSpec{
				Infrastructure: &logging.Infrastructure{
					Sources: []string{logging.InfrastructureSourceContainer},
				},
			},
			nil,
			func() {
				Expect(receiver.ListNamespaces()).To(HaveEach(MatchRegexp("^(openshift.*|kube.*|default)$")))
				Expect(receiver.ListJournalLogs()).To(HaveLen(0), "exp no journal logs to be collected")
			}),
		Entry("application inputs should only collect from matching pod label 'notin' expressions",
			logging.InputSpec{
				Application: &logging.Application{
					Selector: &logging.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key: component, Operator: metav1.LabelSelectorOpNotIn, Values: []string{valueFrontend, valueBackend},
							},
						},
					},
				}},
			nil,
			func() {
				Expect(receiver.ListContainers()).To(Not(HaveEach(MatchRegexp(fmt.Sprintf("^(%s|%s)$", valueFrontend, valueBackend)))))
			}),
		Entry("application inputs should only collect from matching pod label 'in' expressions",
			logging.InputSpec{
				Application: &logging.Application{
					Selector: &logging.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key: component, Operator: metav1.LabelSelectorOpIn, Values: []string{valueFrontend, valueBackend},
							},
						},
					},
				}},
			nil,
			func() {
				Expect(receiver.ListContainers()).To(HaveEach(MatchRegexp(fmt.Sprintf("^(%s|%s)$", valueFrontend, valueBackend))))
			}),
		Entry("application inputs should only collect from matching pod labels",
			logging.InputSpec{
				Application: &logging.Application{
					Selector: &logging.LabelSelector{
						MatchLabels: map[string]string{
							component: valueFrontend,
						},
					},
				}},
			func(component string) string {
				if component == valueFrontend {
					return valueFrontend
				}
				return logGeneratorNameFn(component)
			},
			func() {
				Expect(receiver.ListContainers()).To(HaveEach(valueFrontend))
			}),
		Entry("application inputs should only collect from included namespaces with wildcards",
			logging.InputSpec{
				Application: &logging.Application{
					Namespaces: []string{"clo-test*"},
				}},
			logGeneratorNameFn,
			func() {
				Expect(receiver.ListNamespaces()).To(HaveEach(MatchRegexp("^clo-test.*$")))
			}),
		Entry("application inputs should not collect from excluded namespaces",
			logging.InputSpec{
				Application: &logging.Application{
					ExcludeNamespaces: []string{"clo-test*"},
				}},
			logGeneratorNameFn,
			func() {
				Expect(receiver.ListNamespaces()).To(HaveLen(0), "exp no logs to be collected")
			}),
		Entry("application inputsshould collect from included containers",
			logging.InputSpec{
				Application: &logging.Application{
					Containers: &logging.InclusionSpec{
						Include: []string{"log-*"},
					},
				}},
			logGeneratorNameFn,
			func() {
				Expect(receiver.ListContainers()).To(HaveEach(MatchRegexp("^log-.*$")))
			}),
		Entry("should not collect from excluded containers",
			logging.InputSpec{
				Application: &logging.Application{
					Containers: &logging.InclusionSpec{
						Exclude: []string{"log-*"},
					},
				}},
			logGeneratorNameFn,
			func() {
				Expect(receiver.ListContainers()).To(Not(HaveEach(MatchRegexp("^log-.*$"))))
			}),
	)
})
