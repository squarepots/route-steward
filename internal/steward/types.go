package steward

import "encoding/json"

const (
	InventorySchema   = 2
	SecretIndexSchema = 1
	ObservedSchema    = 1
)

type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Success       bool   `json:"success"`
	Code          string `json:"code"`
	Data          any    `json:"data"`
}

type Inventory struct {
	Schema        int            `json:"schema"`
	Metadata      Metadata       `json:"metadata"`
	Delivery      Delivery       `json:"delivery"`
	Servers       []Server       `json:"servers"`
	Links         []Link         `json:"links"`
	Routes        []Route        `json:"routes"`
	Providers     []Provider     `json:"providers"`
	Profiles      []Profile      `json:"profiles"`
	ClientTargets []ClientTarget `json:"client_targets"`
}

type Metadata struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	RecoveredAt string `json:"recovered_at,omitempty"`
}

type Delivery struct {
	Directory         string `json:"directory"`
	RecoveryDirectory string `json:"recovery_directory"`
}

type Server struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	AccountLabel string   `json:"account_label"`
	InstanceName string   `json:"instance_name"`
	Region       string   `json:"region"`
	Zone         string   `json:"zone"`
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	Roles        []string `json:"roles"`
	Compute      Compute  `json:"compute"`
	Network      Network  `json:"network"`
	SSH          SSH      `json:"ssh"`
	Firewall     Firewall `json:"firewall"`
}

type Compute struct {
	Driver        string `json:"driver"`
	HostOwnership string `json:"host_ownership"`
}

type Network struct {
	PublicIPv4         string  `json:"public_ipv4"`
	IPv4Type           string  `json:"ipv4_type"`
	PrivateIPv4        *string `json:"private_ipv4"`
	PublicIPv6         *string `json:"public_ipv6"`
	ExpectedEgressIPv4 string  `json:"expected_egress_ipv4"`
	ExpectedEgressIPv6 *string `json:"expected_egress_ipv6"`
}

type SSH struct {
	User           string   `json:"user"`
	KeyPath        string   `json:"key_path"`
	AllowedSources []string `json:"allowed_sources"`
}

type Firewall struct {
	Profile string         `json:"profile"`
	Rules   []FirewallRule `json:"rules"`
}

type FirewallRule struct {
	Family       string `json:"family"`
	Protocol     string `json:"protocol"`
	Port         int    `json:"port"`
	EndPort      int    `json:"end_port,omitempty"`
	Source       string `json:"source,omitempty"`
	SourceServer string `json:"source_server,omitempty"`
}

type Link struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Driver         string `json:"driver"`
	EntryServer    string `json:"entry_server"`
	ExitServer     string `json:"exit_server"`
	Slot           int    `json:"slot"`
	Interface      string `json:"interface"`
	ListenPort     int    `json:"listen_port"`
	Subnet         string `json:"subnet"`
	EntryAddress   string `json:"entry_address"`
	ExitAddress    string `json:"exit_address"`
	EndpointFamily string `json:"endpoint_family"`
	SecretRef      string `json:"secret_ref"`
	Enabled        bool   `json:"enabled"`
}

type Route struct {
	ID                  string       `json:"id"`
	DisplayName         string       `json:"display_name"`
	Kind                string       `json:"kind"`
	Ingress             Ingress      `json:"ingress"`
	EntryServer         string       `json:"entry_server"`
	ExitServer          string       `json:"exit_server"`
	Link                *string      `json:"link"`
	ListenPort          int          `json:"listen_port"`
	PortHopping         *PortHopping `json:"port_hopping,omitempty"`
	Enabled             bool         `json:"enabled"`
	Order               int          `json:"order"`
	AddressFamilies     []string     `json:"address_families"`
	PayloadSecretRef    string       `json:"payload_secret_ref"`
	CredentialSecretRef string       `json:"credential_secret_ref"`
	CredentialMode      string       `json:"credential_mode"`
	State               string       `json:"state"`
}

type Ingress struct {
	Driver string `json:"driver"`
}

type Provider struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	SourceType      string `json:"source_type"`
	SourceSecretRef string `json:"source_secret_ref"`
	IntervalSeconds int    `json:"interval_seconds"`
	HealthCheck     bool   `json:"health_check"`
	Enabled         bool   `json:"enabled"`
}

type Profile struct {
	ID               string          `json:"id"`
	IncludeRoutes    []string        `json:"include_routes"`
	IncludeProviders []string        `json:"include_providers"`
	Routing          *ProfileRouting `json:"routing,omitempty"`
}

type ClientTarget struct {
	ID                    string          `json:"id"`
	Profile               string          `json:"profile"`
	Renderer              string          `json:"renderer"`
	Delivery              string          `json:"delivery"`
	MihomoProcessNames    []string        `json:"mihomo_process_names,omitempty"`
	Route                 string          `json:"route,omitempty"`
	Listen                string          `json:"listen,omitempty"`
	IngressFamily         string          `json:"ingress_family,omitempty"`
	QR                    json.RawMessage `json:"qr,omitempty"`
	SubscriptionSecretRef string          `json:"subscription_secret_ref,omitempty"`
}

type SecretIndex struct {
	Schema int                  `json:"schema"`
	Refs   map[string]SecretRef `json:"refs"`
}

type SecretRef struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type ObservedState struct {
	Schema      int              `json:"schema"`
	GeneratedAt *string          `json:"generated_at"`
	Servers     []ObservedObject `json:"servers"`
	Links       []ObservedObject `json:"links"`
	Routes      []ObservedRoute  `json:"routes"`
}

type ObservedObject struct {
	ID          string `json:"id"`
	AuditStatus string `json:"audit_status"`
	AuditedAt   string `json:"audited_at"`
}

type ObservedRoute struct {
	ID               string          `json:"id"`
	AuditStatus      string          `json:"audit_status"`
	Category         string          `json:"category"`
	AuditedAt        string          `json:"audited_at"`
	ActualEgressIPv4 *string         `json:"actual_egress_ipv4"`
	HysteriaVersion  *string         `json:"hysteria_version"`
	WireGuardVersion *string         `json:"wireguard_version"`
	Health           *ObservedHealth `json:"health,omitempty"`
}

type ObservedHealth struct {
	Status    string            `json:"status"`
	CheckedAt string            `json:"checked_at"`
	LatencyMS *int64            `json:"latency_ms,omitempty"`
	Checks    map[string]string `json:"checks"`
}

type HealthCheck struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	LatencyMS *int64 `json:"latency_ms,omitempty"`
}

type HealthResult struct {
	SchemaVersion int           `json:"schema_version"`
	Route         string        `json:"route"`
	Kind          string        `json:"kind"`
	Status        string        `json:"status"`
	Summary       string        `json:"summary"`
	CheckedAt     string        `json:"checked_at"`
	LatencyMS     *int64        `json:"latency_ms,omitempty"`
	Checks        []HealthCheck `json:"checks"`
	PublicIPv4    *string       `json:"public_ipv4,omitempty"`
	PublicIPv6    *string       `json:"public_ipv6,omitempty"`
}

type ContextField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Source   string `json:"source,omitempty"`
	When     string `json:"when,omitempty"`
}

type Capability struct {
	ID                        string         `json:"id"`
	State                     string         `json:"state"`
	Executor                  string         `json:"executor"`
	Mutation                  bool           `json:"mutation"`
	AuthorizationClass        string         `json:"authorization_class"`
	RequiresLocalSecretPrompt bool           `json:"requires_local_secret_prompt,omitempty"`
	Description               string         `json:"description"`
	RequiredContext           []ContextField `json:"required_context"`
	Effects                   []string       `json:"effects"`
}

type Preflight struct {
	SchemaVersion             int      `json:"schema_version"`
	Operation                 string   `json:"operation"`
	Target                    *string  `json:"target"`
	State                     string   `json:"state"`
	Executor                  string   `json:"executor"`
	Mutation                  bool     `json:"mutation"`
	AuthorizationClass        string   `json:"authorization_class"`
	ContextComplete           bool     `json:"context_complete"`
	Authorized                bool     `json:"authorized"`
	Ready                     bool     `json:"ready"`
	MissingContext            []string `json:"missing_context"`
	Conflicts                 []string `json:"conflicts"`
	UserDecisions             []string `json:"user_decisions"`
	ExpectedEffects           []string `json:"expected_effects"`
	RequiresLocalSecretPrompt bool     `json:"requires_local_secret_prompt,omitempty"`
}

type RenderOutput struct {
	ClientTarget  string    `json:"client_target"`
	Profile       string    `json:"profile"`
	Renderer      string    `json:"renderer"`
	Path          string    `json:"path,omitempty"`
	NodeCount     int       `json:"node_count"`
	ProviderCount int       `json:"provider_count,omitempty"`
	Subscription  bool      `json:"subscription,omitempty"`
	Validation    string    `json:"validation"`
	Artifact      *Artifact `json:"artifact,omitempty"`
}

type Artifact struct {
	ID           string `json:"id"`
	FileName     string `json:"file_name"`
	RelativePath string `json:"relative_path"`
}

type RenderResult struct {
	SchemaVersion int            `json:"schema_version"`
	Command       string         `json:"command"`
	Success       bool           `json:"success"`
	Outputs       []RenderOutput `json:"outputs"`
}

type ClientProxyCheckResult struct {
	SchemaVersion int    `json:"schema_version"`
	ClientTarget  string `json:"client_target"`
	Route         string `json:"route"`
	Status        string `json:"status"`
	Detail        string `json:"detail"`
	HTTPProxy     string `json:"http_proxy"`
	SOCKS5Proxy   string `json:"socks5_proxy"`
	ClientVersion string `json:"client_version"`
	Downloaded    bool   `json:"downloaded"`
}

func (link *Link) UnmarshalJSON(data []byte) error {
	type alias Link
	decoded := struct {
		Enabled *bool `json:"enabled"`
		*alias
	}{alias: (*alias)(link)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	link.Enabled = decoded.Enabled == nil || *decoded.Enabled
	return nil
}

func (route *Route) UnmarshalJSON(data []byte) error {
	type alias Route
	decoded := struct {
		Enabled *bool `json:"enabled"`
		*alias
	}{alias: (*alias)(route)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	route.Enabled = decoded.Enabled == nil || *decoded.Enabled
	return nil
}

func (provider *Provider) UnmarshalJSON(data []byte) error {
	type alias Provider
	decoded := struct {
		Enabled *bool `json:"enabled"`
		*alias
	}{alias: (*alias)(provider)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	provider.Enabled = decoded.Enabled == nil || *decoded.Enabled
	return nil
}

func (profile *Profile) UnmarshalJSON(data []byte) error {
	type alias Profile
	decoded := struct {
		IncludeRoutes    *[]string `json:"include_routes"`
		IncludeProviders *[]string `json:"include_providers"`
		*alias
	}{alias: (*alias)(profile)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.IncludeRoutes == nil {
		profile.IncludeRoutes = []string{"*"}
	} else {
		profile.IncludeRoutes = append([]string{}, (*decoded.IncludeRoutes)...)
	}
	if decoded.IncludeProviders == nil {
		profile.IncludeProviders = []string{}
	} else {
		profile.IncludeProviders = append([]string{}, (*decoded.IncludeProviders)...)
	}
	return nil
}
