package v1

// NOTE: The Enum validation on ReceiverSpec.Type must be updated if the list of types changes.

// Receiver type constants, must match JSON tags of OutputTypeSpec fields.
const (
	ReceiverTypeHttp   = "http"
	ReceiverTypeSyslog = "syslog"

	FormatKubeAPIAudit = "kubeAPIAudit" // Log events in k8s list format, e.g. API audit log events.
)

// ReceiverSpec is a union of input Receiver types.
//
// The fields of this struct define the set of known Receiver types.
type ReceiverSpec struct {

	// Type of Receiver plugin.
	//
	// +kubebuilder:validation:Enum:=http;syslog
	// +required
	Type string `json:"type"`

	// The ReceiverTypeSpec that handles particular parameters
	*ReceiverTypeSpec `json:",inline"`
}

type ReceiverTypeSpec struct {
	HTTP   *HTTPReceiver   `json:"http,omitempty"`
	Syslog *SyslogReceiver `json:"syslog,omitempty"`
}

// HTTPReceiver receives encoded logs as a HTTP endpoint.
type HTTPReceiver struct {
	// Port the Receiver listens on.
	// +kubebuilder:default:=8443
	// +optional
	Port int32 `json:"port"`

	// Format is the format of incoming log data.
	//
	// +kubebuilder:validation:Enum:=kubeAPIAudit
	// +required
	Format string `json:"format"`
}

// SyslogReceiver receives logs from rsyslog
type SyslogReceiver struct {
	// Port the Receiver listens on.
	// +kubebuilder:default:=10514
	// +optional
	Port int32 `json:"port"`

	// The protocol of the connection the Receiver will listen on: tcp or upd
	// +kubebuilder:validation:Enum=tcp;udp
	// +kubebuilder:default:=tcp
	// +optional
	Protocol string `json:"protocol"`
}
