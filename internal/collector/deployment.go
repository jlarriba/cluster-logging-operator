package collector

import (
	"context"
	"fmt"

	log "github.com/ViaQ/logerr/v2/log/static"
	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/internal/reconcile"
	"github.com/openshift/cluster-logging-operator/internal/runtime"
	"github.com/openshift/cluster-logging-operator/internal/tls"
	"github.com/openshift/cluster-logging-operator/internal/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileDeployment reconciles a deployment specifically for the collector defined by the factory
func (f *Factory) ReconcileDeployment(er record.EventRecorder, k8sClient client.Client, namespace string, owner metav1.OwnerReference) error {
	trustedCABundle, trustHash := GetTrustedCABundle(k8sClient, namespace, f.ResourceNames.CaTrustBundle)
	f.TrustedCAHash = trustHash
	tlsProfile, _ := tls.FetchAPIServerTlsProfile(k8sClient)

	var receiverInputs []string
	for _, input := range f.ForwarderSpec.Inputs {
		if logging.IsHttpReceiver(&input) || logging.IsSyslogReceiver(&input) {
			receiverInputs = append(receiverInputs, f.ResourceNames.GenerateInputServiceName(input.Name))
		}
	}

	desired := f.NewDeployment(namespace, f.ResourceNames.DaemonSetName(), trustedCABundle, tls.GetClusterTLSProfileSpec(tlsProfile), receiverInputs)
	utils.AddOwnerRefToObject(desired, owner)
	return reconcile.Deployment(er, k8sClient, desired)
}

func RemoveDeployment(k8sClient client.Client, namespace, name string) (err error) {
	log.V(3).Info("Removing collector deployment", "namespace", namespace, "name", name)
	ds := runtime.NewDeployment(namespace, name)
	if err = k8sClient.Delete(context.TODO(), ds); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failure deleting deployment %s/%s: %v", namespace, name, err)
	}
	return nil
}
