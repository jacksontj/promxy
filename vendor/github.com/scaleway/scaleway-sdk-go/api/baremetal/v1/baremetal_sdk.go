// This file was automatically generated. DO NOT EDIT.
// If you have any remark or suggestion do not hesitate to open an issue.

// Package baremetal provides methods and message types of the baremetal v1 API.
package baremetal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/scaleway/scaleway-sdk-go/internal/errors"
	"github.com/scaleway/scaleway-sdk-go/internal/marshaler"
	"github.com/scaleway/scaleway-sdk-go/internal/parameter"
	"github.com/scaleway/scaleway-sdk-go/namegenerator"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

// always import dependencies
var (
	_ fmt.Stringer
	_ json.Unmarshaler
	_ url.URL
	_ net.IP
	_ http.Header
	_ bytes.Reader
	_ time.Time
	_ = strings.Join

	_ scw.ScalewayRequest
	_ marshaler.Duration
	_ scw.File
	_ = parameter.AddToQuery
	_ = namegenerator.GetRandomName
)

type IPReverseStatus string

const (
	IPReverseStatusUnknown = IPReverseStatus("unknown")
	IPReverseStatusPending = IPReverseStatus("pending")
	IPReverseStatusActive  = IPReverseStatus("active")
	IPReverseStatusError   = IPReverseStatus("error")
)

func (enum IPReverseStatus) String() string {
	if enum == "" {
		// return default value if empty
		return "unknown"
	}
	return string(enum)
}

func (enum IPReverseStatus) Values() []IPReverseStatus {
	return []IPReverseStatus{
		"unknown",
		"pending",
		"active",
		"error",
	}
}

func (enum IPReverseStatus) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *IPReverseStatus) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = IPReverseStatus(IPReverseStatus(tmp).String())
	return nil
}

type IPVersion string

const (
	IPVersionIPv4 = IPVersion("IPv4")
	IPVersionIPv6 = IPVersion("IPv6")
)

func (enum IPVersion) String() string {
	if enum == "" {
		// return default value if empty
		return "IPv4"
	}
	return string(enum)
}

func (enum IPVersion) Values() []IPVersion {
	return []IPVersion{
		"IPv4",
		"IPv6",
	}
}

func (enum IPVersion) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *IPVersion) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = IPVersion(IPVersion(tmp).String())
	return nil
}

type ListServerEventsRequestOrderBy string

const (
	ListServerEventsRequestOrderByCreatedAtAsc  = ListServerEventsRequestOrderBy("created_at_asc")
	ListServerEventsRequestOrderByCreatedAtDesc = ListServerEventsRequestOrderBy("created_at_desc")
)

func (enum ListServerEventsRequestOrderBy) String() string {
	if enum == "" {
		// return default value if empty
		return "created_at_asc"
	}
	return string(enum)
}

func (enum ListServerEventsRequestOrderBy) Values() []ListServerEventsRequestOrderBy {
	return []ListServerEventsRequestOrderBy{
		"created_at_asc",
		"created_at_desc",
	}
}

func (enum ListServerEventsRequestOrderBy) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ListServerEventsRequestOrderBy) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ListServerEventsRequestOrderBy(ListServerEventsRequestOrderBy(tmp).String())
	return nil
}

type ListServerPrivateNetworksRequestOrderBy string

const (
	ListServerPrivateNetworksRequestOrderByCreatedAtAsc  = ListServerPrivateNetworksRequestOrderBy("created_at_asc")
	ListServerPrivateNetworksRequestOrderByCreatedAtDesc = ListServerPrivateNetworksRequestOrderBy("created_at_desc")
	ListServerPrivateNetworksRequestOrderByUpdatedAtAsc  = ListServerPrivateNetworksRequestOrderBy("updated_at_asc")
	ListServerPrivateNetworksRequestOrderByUpdatedAtDesc = ListServerPrivateNetworksRequestOrderBy("updated_at_desc")
)

func (enum ListServerPrivateNetworksRequestOrderBy) String() string {
	if enum == "" {
		// return default value if empty
		return "created_at_asc"
	}
	return string(enum)
}

func (enum ListServerPrivateNetworksRequestOrderBy) Values() []ListServerPrivateNetworksRequestOrderBy {
	return []ListServerPrivateNetworksRequestOrderBy{
		"created_at_asc",
		"created_at_desc",
		"updated_at_asc",
		"updated_at_desc",
	}
}

func (enum ListServerPrivateNetworksRequestOrderBy) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ListServerPrivateNetworksRequestOrderBy) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ListServerPrivateNetworksRequestOrderBy(ListServerPrivateNetworksRequestOrderBy(tmp).String())
	return nil
}

type ListServersRequestOrderBy string

const (
	ListServersRequestOrderByCreatedAtAsc  = ListServersRequestOrderBy("created_at_asc")
	ListServersRequestOrderByCreatedAtDesc = ListServersRequestOrderBy("created_at_desc")
)

func (enum ListServersRequestOrderBy) String() string {
	if enum == "" {
		// return default value if empty
		return "created_at_asc"
	}
	return string(enum)
}

func (enum ListServersRequestOrderBy) Values() []ListServersRequestOrderBy {
	return []ListServersRequestOrderBy{
		"created_at_asc",
		"created_at_desc",
	}
}

func (enum ListServersRequestOrderBy) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ListServersRequestOrderBy) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ListServersRequestOrderBy(ListServersRequestOrderBy(tmp).String())
	return nil
}

type ListSettingsRequestOrderBy string

const (
	ListSettingsRequestOrderByCreatedAtAsc  = ListSettingsRequestOrderBy("created_at_asc")
	ListSettingsRequestOrderByCreatedAtDesc = ListSettingsRequestOrderBy("created_at_desc")
)

func (enum ListSettingsRequestOrderBy) String() string {
	if enum == "" {
		// return default value if empty
		return "created_at_asc"
	}
	return string(enum)
}

func (enum ListSettingsRequestOrderBy) Values() []ListSettingsRequestOrderBy {
	return []ListSettingsRequestOrderBy{
		"created_at_asc",
		"created_at_desc",
	}
}

func (enum ListSettingsRequestOrderBy) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ListSettingsRequestOrderBy) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ListSettingsRequestOrderBy(ListSettingsRequestOrderBy(tmp).String())
	return nil
}

type OfferStock string

const (
	OfferStockEmpty     = OfferStock("empty")
	OfferStockLow       = OfferStock("low")
	OfferStockAvailable = OfferStock("available")
)

func (enum OfferStock) String() string {
	if enum == "" {
		// return default value if empty
		return "empty"
	}
	return string(enum)
}

func (enum OfferStock) Values() []OfferStock {
	return []OfferStock{
		"empty",
		"low",
		"available",
	}
}

func (enum OfferStock) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *OfferStock) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = OfferStock(OfferStock(tmp).String())
	return nil
}

type OfferSubscriptionPeriod string

const (
	OfferSubscriptionPeriodUnknownSubscriptionPeriod = OfferSubscriptionPeriod("unknown_subscription_period")
	OfferSubscriptionPeriodHourly                    = OfferSubscriptionPeriod("hourly")
	OfferSubscriptionPeriodMonthly                   = OfferSubscriptionPeriod("monthly")
)

func (enum OfferSubscriptionPeriod) String() string {
	if enum == "" {
		// return default value if empty
		return "unknown_subscription_period"
	}
	return string(enum)
}

func (enum OfferSubscriptionPeriod) Values() []OfferSubscriptionPeriod {
	return []OfferSubscriptionPeriod{
		"unknown_subscription_period",
		"hourly",
		"monthly",
	}
}

func (enum OfferSubscriptionPeriod) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *OfferSubscriptionPeriod) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = OfferSubscriptionPeriod(OfferSubscriptionPeriod(tmp).String())
	return nil
}

type ServerBootType string

const (
	ServerBootTypeUnknownBootType = ServerBootType("unknown_boot_type")
	ServerBootTypeNormal          = ServerBootType("normal")
	ServerBootTypeRescue          = ServerBootType("rescue")
)

func (enum ServerBootType) String() string {
	if enum == "" {
		// return default value if empty
		return "unknown_boot_type"
	}
	return string(enum)
}

func (enum ServerBootType) Values() []ServerBootType {
	return []ServerBootType{
		"unknown_boot_type",
		"normal",
		"rescue",
	}
}

func (enum ServerBootType) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ServerBootType) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ServerBootType(ServerBootType(tmp).String())
	return nil
}

type ServerInstallStatus string

const (
	ServerInstallStatusUnknown    = ServerInstallStatus("unknown")
	ServerInstallStatusToInstall  = ServerInstallStatus("to_install")
	ServerInstallStatusInstalling = ServerInstallStatus("installing")
	ServerInstallStatusCompleted  = ServerInstallStatus("completed")
	ServerInstallStatusError      = ServerInstallStatus("error")
)

func (enum ServerInstallStatus) String() string {
	if enum == "" {
		// return default value if empty
		return "unknown"
	}
	return string(enum)
}

func (enum ServerInstallStatus) Values() []ServerInstallStatus {
	return []ServerInstallStatus{
		"unknown",
		"to_install",
		"installing",
		"completed",
		"error",
	}
}

func (enum ServerInstallStatus) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ServerInstallStatus) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ServerInstallStatus(ServerInstallStatus(tmp).String())
	return nil
}

type ServerOptionOptionStatus string

const (
	ServerOptionOptionStatusOptionStatusUnknown   = ServerOptionOptionStatus("option_status_unknown")
	ServerOptionOptionStatusOptionStatusEnable    = ServerOptionOptionStatus("option_status_enable")
	ServerOptionOptionStatusOptionStatusEnabling  = ServerOptionOptionStatus("option_status_enabling")
	ServerOptionOptionStatusOptionStatusDisabling = ServerOptionOptionStatus("option_status_disabling")
	ServerOptionOptionStatusOptionStatusError     = ServerOptionOptionStatus("option_status_error")
)

func (enum ServerOptionOptionStatus) String() string {
	if enum == "" {
		// return default value if empty
		return "option_status_unknown"
	}
	return string(enum)
}

func (enum ServerOptionOptionStatus) Values() []ServerOptionOptionStatus {
	return []ServerOptionOptionStatus{
		"option_status_unknown",
		"option_status_enable",
		"option_status_enabling",
		"option_status_disabling",
		"option_status_error",
	}
}

func (enum ServerOptionOptionStatus) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ServerOptionOptionStatus) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ServerOptionOptionStatus(ServerOptionOptionStatus(tmp).String())
	return nil
}

type ServerPingStatus string

const (
	ServerPingStatusPingStatusUnknown = ServerPingStatus("ping_status_unknown")
	ServerPingStatusPingStatusUp      = ServerPingStatus("ping_status_up")
	ServerPingStatusPingStatusDown    = ServerPingStatus("ping_status_down")
)

func (enum ServerPingStatus) String() string {
	if enum == "" {
		// return default value if empty
		return "ping_status_unknown"
	}
	return string(enum)
}

func (enum ServerPingStatus) Values() []ServerPingStatus {
	return []ServerPingStatus{
		"ping_status_unknown",
		"ping_status_up",
		"ping_status_down",
	}
}

func (enum ServerPingStatus) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ServerPingStatus) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ServerPingStatus(ServerPingStatus(tmp).String())
	return nil
}

type ServerPrivateNetworkStatus string

const (
	ServerPrivateNetworkStatusUnknown   = ServerPrivateNetworkStatus("unknown")
	ServerPrivateNetworkStatusAttaching = ServerPrivateNetworkStatus("attaching")
	ServerPrivateNetworkStatusAttached  = ServerPrivateNetworkStatus("attached")
	ServerPrivateNetworkStatusError     = ServerPrivateNetworkStatus("error")
	ServerPrivateNetworkStatusDetaching = ServerPrivateNetworkStatus("detaching")
	ServerPrivateNetworkStatusLocked    = ServerPrivateNetworkStatus("locked")
)

func (enum ServerPrivateNetworkStatus) String() string {
	if enum == "" {
		// return default value if empty
		return "unknown"
	}
	return string(enum)
}

func (enum ServerPrivateNetworkStatus) Values() []ServerPrivateNetworkStatus {
	return []ServerPrivateNetworkStatus{
		"unknown",
		"attaching",
		"attached",
		"error",
		"detaching",
		"locked",
	}
}

func (enum ServerPrivateNetworkStatus) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ServerPrivateNetworkStatus) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ServerPrivateNetworkStatus(ServerPrivateNetworkStatus(tmp).String())
	return nil
}

type ServerStatus string

const (
	ServerStatusUnknown    = ServerStatus("unknown")
	ServerStatusDelivering = ServerStatus("delivering")
	ServerStatusReady      = ServerStatus("ready")
	ServerStatusStopping   = ServerStatus("stopping")
	ServerStatusStopped    = ServerStatus("stopped")
	ServerStatusStarting   = ServerStatus("starting")
	ServerStatusError      = ServerStatus("error")
	ServerStatusDeleting   = ServerStatus("deleting")
	ServerStatusLocked     = ServerStatus("locked")
	ServerStatusOutOfStock = ServerStatus("out_of_stock")
	ServerStatusOrdered    = ServerStatus("ordered")
	ServerStatusResetting  = ServerStatus("resetting")
)

func (enum ServerStatus) String() string {
	if enum == "" {
		// return default value if empty
		return "unknown"
	}
	return string(enum)
}

func (enum ServerStatus) Values() []ServerStatus {
	return []ServerStatus{
		"unknown",
		"delivering",
		"ready",
		"stopping",
		"stopped",
		"starting",
		"error",
		"deleting",
		"locked",
		"out_of_stock",
		"ordered",
		"resetting",
	}
}

func (enum ServerStatus) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *ServerStatus) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = ServerStatus(ServerStatus(tmp).String())
	return nil
}

type SettingType string

const (
	SettingTypeUnknown = SettingType("unknown")
	SettingTypeSMTP    = SettingType("smtp")
)

func (enum SettingType) String() string {
	if enum == "" {
		// return default value if empty
		return "unknown"
	}
	return string(enum)
}

func (enum SettingType) Values() []SettingType {
	return []SettingType{
		"unknown",
		"smtp",
	}
}

func (enum SettingType) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, enum)), nil
}

func (enum *SettingType) UnmarshalJSON(data []byte) error {
	tmp := ""

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	*enum = SettingType(SettingType(tmp).String())
	return nil
}

// OSOSField: osos field.
type OSOSField struct {
	Editable bool `json:"editable"`

	Required bool `json:"required"`

	DefaultValue *string `json:"default_value"`
}

// CPU: cpu.
type CPU struct {
	// Name: name of the CPU.
	Name string `json:"name"`

	// CoreCount: number of CPU cores.
	CoreCount uint32 `json:"core_count"`

	// ThreadCount: number CPU threads.
	ThreadCount uint32 `json:"thread_count"`

	// Frequency: frequency of the CPU in MHz.
	Frequency uint32 `json:"frequency"`

	// Benchmark: benchmark of the CPU.
	Benchmark string `json:"benchmark"`
}

// Disk: disk.
type Disk struct {
	// Capacity: capacity of the disk in bytes.
	Capacity scw.Size `json:"capacity"`

	// Type: type of the disk.
	Type string `json:"type"`
}

// Memory: memory.
type Memory struct {
	// Capacity: capacity of the memory in bytes.
	Capacity scw.Size `json:"capacity"`

	// Type: type of the memory.
	Type string `json:"type"`

	// Frequency: frequency of the memory in MHz.
	Frequency uint32 `json:"frequency"`

	// IsEcc: true if the memory is an error-correcting code memory.
	IsEcc bool `json:"is_ecc"`
}

// OfferOptionOffer: offer option offer.
type OfferOptionOffer struct {
	// ID: ID of the option.
	ID string `json:"id"`

	// Name: name of the option.
	Name string `json:"name"`

	// Enabled: if true the option is enabled and included by default in the offer
	// If false the option is available for the offer but not included by default.
	Enabled bool `json:"enabled"`

	// SubscriptionPeriod: period of subscription for the offer.
	// Default value: unknown_subscription_period
	SubscriptionPeriod OfferSubscriptionPeriod `json:"subscription_period"`

	// Price: price of the option.
	Price *scw.Money `json:"price"`

	// Manageable: boolean to know if option could be managed.
	Manageable bool `json:"manageable"`

	// OsID: ID of the OS linked to the option.
	OsID *string `json:"os_id"`
}

// PersistentMemory: persistent memory.
type PersistentMemory struct {
	// Capacity: capacity of the memory in bytes.
	Capacity scw.Size `json:"capacity"`

	// Type: type of the memory.
	Type string `json:"type"`

	// Frequency: frequency of the memory in MHz.
	Frequency uint32 `json:"frequency"`
}

// RaidController: raid controller.
type RaidController struct {
	Model string `json:"model"`

	RaidLevel []string `json:"raid_level"`
}

// IP: ip.
type IP struct {
	// ID: ID of the IP.
	ID string `json:"id"`

	// Address: address of the IP.
	Address net.IP `json:"address"`

	// Reverse: reverse IP value.
	Reverse string `json:"reverse"`

	// Version: version of IP (v4 or v6).
	// Default value: IPv4
	Version IPVersion `json:"version"`

	// ReverseStatus: status of the reverse.
	// Default value: unknown
	ReverseStatus IPReverseStatus `json:"reverse_status"`

	// ReverseStatusMessage: a message related to the reverse status, e.g. in case of an error.
	ReverseStatusMessage string `json:"reverse_status_message"`
}

// ServerInstall: server install.
type ServerInstall struct {
	// OsID: ID of the OS.
	OsID string `json:"os_id"`

	// Hostname: host defined during the server installation.
	Hostname string `json:"hostname"`

	// SSHKeyIDs: SSH public key IDs defined during server installation.
	SSHKeyIDs []string `json:"ssh_key_ids"`

	// Status: status of the server installation.
	// Default value: unknown
	Status ServerInstallStatus `json:"status"`

	// User: user defined in the server installation, or the default user if none were specified.
	User string `json:"user"`

	// ServiceUser: service user defined in the server installation, or the default user if none were specified.
	ServiceUser string `json:"service_user"`

	// ServiceURL: address of the installed service.
	ServiceURL string `json:"service_url"`
}

// ServerOption: server option.
type ServerOption struct {
	// ID: ID of the option.
	ID string `json:"id"`

	// Name: name of the option.
	Name string `json:"name"`

	// Status: status of the option on this server.
	// Default value: option_status_unknown
	Status ServerOptionOptionStatus `json:"status"`

	// Manageable: defines whether the option can be managed (added or removed).
	Manageable bool `json:"manageable"`

	// ExpiresAt: auto expiration date for compatible options.
	ExpiresAt *time.Time `json:"expires_at"`
}

// ServerRescueServer: server rescue server.
type ServerRescueServer struct {
	// User: rescue user name.
	User string `json:"user"`

	// Password: rescue password.
	Password string `json:"password"`
}

// CreateServerRequestInstall: create server request install.
type CreateServerRequestInstall struct {
	// OsID: ID of the OS to installation on the server.
	OsID string `json:"os_id"`

	// Hostname: hostname of the server.
	Hostname string `json:"hostname"`

	// SSHKeyIDs: SSH key IDs authorized on the server.
	SSHKeyIDs []string `json:"ssh_key_ids"`

	// User: user for the installation.
	User *string `json:"user"`

	// Password: password for the installation.
	Password *string `json:"password"`

	// ServiceUser: regular user that runs the service to be installed on the server.
	ServiceUser *string `json:"service_user"`

	// ServicePassword: password used for the service to install.
	ServicePassword *string `json:"service_password"`
}

// OS: os.
type OS struct {
	// ID: ID of the OS.
	ID string `json:"id"`

	// Name: name of the OS.
	Name string `json:"name"`

	// Version: version of the OS.
	Version string `json:"version"`

	// LogoURL: URL of this OS's logo.
	LogoURL string `json:"logo_url"`

	// SSH: object defining the SSH requirements to install the OS.
	SSH *OSOSField `json:"ssh"`

	// User: object defining the username requirements to install the OS.
	User *OSOSField `json:"user"`

	// Password: object defining the password requirements to install the OS.
	Password *OSOSField `json:"password"`

	// ServiceUser: object defining the username requirements to install the service.
	ServiceUser *OSOSField `json:"service_user"`

	// ServicePassword: object defining the password requirements to install the service.
	ServicePassword *OSOSField `json:"service_password"`

	// Enabled: defines if the operating system is enabled or not.
	Enabled bool `json:"enabled"`

	// LicenseRequired: license required (check server options for pricing details).
	LicenseRequired bool `json:"license_required"`

	// Allowed: defines if a specific Organization is allowed to install this OS type.
	Allowed bool `json:"allowed"`
}

// Offer: offer.
type Offer struct {
	// ID: ID of the offer.
	ID string `json:"id"`

	// Name: name of the offer.
	Name string `json:"name"`

	// Stock: stock level.
	// Default value: empty
	Stock OfferStock `json:"stock"`

	// Bandwidth: public bandwidth available (in bits/s) with the offer.
	Bandwidth uint64 `json:"bandwidth"`

	// MaxBandwidth: maximum public bandwidth available (in bits/s) depending on available options.
	MaxBandwidth uint64 `json:"max_bandwidth"`

	// CommercialRange: commercial range of the offer.
	CommercialRange string `json:"commercial_range"`

	// PricePerHour: price of the offer for the next 60 minutes (a server order at 11h32 will be payed until 12h32).
	PricePerHour *scw.Money `json:"price_per_hour"`

	// PricePerMonth: monthly price of the offer, if subscribing on a monthly basis.
	PricePerMonth *scw.Money `json:"price_per_month"`

	// Disks: disks specifications of the offer.
	Disks []*Disk `json:"disks"`

	// Enable: defines whether the offer is currently available.
	Enable bool `json:"enable"`

	// CPUs: CPU specifications of the offer.
	CPUs []*CPU `json:"cpus"`

	// Memories: memory specifications of the offer.
	Memories []*Memory `json:"memories"`

	// QuotaName: name of the quota associated to the offer.
	QuotaName string `json:"quota_name"`

	// PersistentMemories: persistent memory specifications of the offer.
	PersistentMemories []*PersistentMemory `json:"persistent_memories"`

	// RaidControllers: raid controller specifications of the offer.
	RaidControllers []*RaidController `json:"raid_controllers"`

	// IncompatibleOsIDs: array of OS images IDs incompatible with the server.
	IncompatibleOsIDs []string `json:"incompatible_os_ids"`

	// SubscriptionPeriod: period of subscription for the offer.
	// Default value: unknown_subscription_period
	SubscriptionPeriod OfferSubscriptionPeriod `json:"subscription_period"`

	// OperationPath: operation path of the service.
	OperationPath string `json:"operation_path"`

	// Fee: one time fee invoiced by Scaleway for the setup and activation of the server.
	Fee *scw.Money `json:"fee"`

	// Options: available options for customization of the server.
	Options []*OfferOptionOffer `json:"options"`

	// PrivateBandwidth: private bandwidth available in bits/s with the offer.
	PrivateBandwidth uint64 `json:"private_bandwidth"`

	// SharedBandwidth: defines whether the offer's bandwidth is shared or not.
	SharedBandwidth bool `json:"shared_bandwidth"`

	// Tags: array of tags attached to the offer.
	Tags []string `json:"tags"`
}

// Option: option.
type Option struct {
	// ID: ID of the option.
	ID string `json:"id"`

	// Name: name of the option.
	Name string `json:"name"`

	// Manageable: defines whether the option is manageable (could be added or removed).
	Manageable bool `json:"manageable"`
}

// ServerEvent: server event.
type ServerEvent struct {
	// ID: ID of the server to which the action will be applied.
	ID string `json:"id"`

	// Action: the action that will be applied to the server.
	Action string `json:"action"`

	// UpdatedAt: date of last modification of the action.
	UpdatedAt *time.Time `json:"updated_at"`

	// CreatedAt: date of creation of the action.
	CreatedAt *time.Time `json:"created_at"`
}

// ServerPrivateNetwork: server private network.
type ServerPrivateNetwork struct {
	// ID: the Private Network ID.
	ID string `json:"id"`

	// ProjectID: the Private Network Project ID.
	ProjectID string `json:"project_id"`

	// ServerID: the server ID.
	ServerID string `json:"server_id"`

	// PrivateNetworkID: the Private Network ID.
	PrivateNetworkID string `json:"private_network_id"`

	// Vlan: the VLAN ID associated to the Private Network.
	Vlan *uint32 `json:"vlan"`

	// Status: the configuration status of the Private Network.
	// Default value: unknown
	Status ServerPrivateNetworkStatus `json:"status"`

	// CreatedAt: the Private Network creation date.
	CreatedAt *time.Time `json:"created_at"`

	// UpdatedAt: the date the Private Network was last modified.
	UpdatedAt *time.Time `json:"updated_at"`
}

// Server: server.
type Server struct {
	// ID: ID of the server.
	ID string `json:"id"`

	// OrganizationID: organization ID the server is attached to.
	OrganizationID string `json:"organization_id"`

	// ProjectID: project ID the server is attached to.
	ProjectID string `json:"project_id"`

	// Name: name of the server.
	Name string `json:"name"`

	// Description: description of the server.
	Description string `json:"description"`

	// UpdatedAt: last modification date of the server.
	UpdatedAt *time.Time `json:"updated_at"`

	// CreatedAt: creation date of the server.
	CreatedAt *time.Time `json:"created_at"`

	// Status: status of the server.
	// Default value: unknown
	Status ServerStatus `json:"status"`

	// OfferID: offer ID of the server.
	OfferID string `json:"offer_id"`

	// OfferName: offer name of the server.
	OfferName string `json:"offer_name"`

	// Tags: array of custom tags attached to the server.
	Tags []string `json:"tags"`

	// IPs: array of IPs attached to the server.
	IPs []*IP `json:"ips"`

	// Domain: domain of the server.
	Domain string `json:"domain"`

	// BootType: boot type of the server.
	// Default value: unknown_boot_type
	BootType ServerBootType `json:"boot_type"`

	// Zone: zone in which is the server located.
	Zone scw.Zone `json:"zone"`

	// Install: configuration of the installation.
	Install *ServerInstall `json:"install"`

	// PingStatus: status of server ping.
	// Default value: ping_status_unknown
	PingStatus ServerPingStatus `json:"ping_status"`

	// Options: options enabled on the server.
	Options []*ServerOption `json:"options"`

	// RescueServer: configuration of rescue boot.
	RescueServer *ServerRescueServer `json:"rescue_server"`
}

// Setting: setting.
type Setting struct {
	// ID: ID of the setting.
	ID string `json:"id"`

	// Type: type of the setting.
	// Default value: unknown
	Type SettingType `json:"type"`

	// ProjectID: ID of the Project ID.
	ProjectID string `json:"project_id"`

	// Enabled: defines whether the setting is enabled.
	Enabled bool `json:"enabled"`
}

// AddOptionServerRequest: add option server request.
type AddOptionServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server.
	ServerID string `json:"-"`

	// OptionID: ID of the option to add.
	OptionID string `json:"-"`

	// ExpiresAt: auto expire the option after this date.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// BMCAccess: bmc access.
type BMCAccess struct {
	// URL: URL to access to the server console.
	URL string `json:"url"`

	// Login: the login to use for the BMC (Baseboard Management Controller) access authentification.
	Login string `json:"login"`

	// Password: the password to use for the BMC (Baseboard Management Controller) access authentification.
	Password string `json:"password"`

	// ExpiresAt: the date after which the BMC (Baseboard Management Controller) access will be closed.
	ExpiresAt *time.Time `json:"expires_at"`
}

// CreateServerRequest: create server request.
type CreateServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// OfferID: offer ID of the new server.
	OfferID string `json:"offer_id"`

	// Deprecated: OrganizationID: organization ID with which the server will be created.
	// Precisely one of ProjectID, OrganizationID must be set.
	OrganizationID *string `json:"organization_id,omitempty"`

	// ProjectID: project ID with which the server will be created.
	// Precisely one of ProjectID, OrganizationID must be set.
	ProjectID *string `json:"project_id,omitempty"`

	// Name: name of the server (≠hostname).
	Name string `json:"name"`

	// Description: description associated with the server, max 255 characters.
	Description string `json:"description"`

	// Tags: tags to associate to the server.
	Tags []string `json:"tags"`

	// Install: object describing the configuration details of the OS installation on the server.
	Install *CreateServerRequestInstall `json:"install,omitempty"`

	// OptionIDs: iDs of options to enable on server.
	OptionIDs []string `json:"option_ids"`
}

// DeleteOptionServerRequest: delete option server request.
type DeleteOptionServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server.
	ServerID string `json:"-"`

	// OptionID: ID of the option to delete.
	OptionID string `json:"-"`
}

// DeleteServerRequest: delete server request.
type DeleteServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server to delete.
	ServerID string `json:"-"`
}

// GetBMCAccessRequest: get bmc access request.
type GetBMCAccessRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server.
	ServerID string `json:"-"`
}

// GetOSRequest: get os request.
type GetOSRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// OsID: ID of the OS.
	OsID string `json:"-"`
}

// GetOfferRequest: get offer request.
type GetOfferRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// OfferID: ID of the researched Offer.
	OfferID string `json:"-"`
}

// GetOptionRequest: get option request.
type GetOptionRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// OptionID: ID of the option.
	OptionID string `json:"-"`
}

// GetServerMetricsRequest: get server metrics request.
type GetServerMetricsRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: server ID to get the metrics.
	ServerID string `json:"-"`
}

// GetServerMetricsResponse: get server metrics response.
type GetServerMetricsResponse struct {
	// Pings: timeseries object representing pings on the server.
	Pings *scw.TimeSeries `json:"pings"`
}

// GetServerRequest: get server request.
type GetServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server.
	ServerID string `json:"-"`
}

// InstallServerRequest: install server request.
type InstallServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: server ID to install.
	ServerID string `json:"-"`

	// OsID: ID of the OS to installation on the server.
	OsID string `json:"os_id"`

	// Hostname: hostname of the server.
	Hostname string `json:"hostname"`

	// SSHKeyIDs: SSH key IDs authorized on the server.
	SSHKeyIDs []string `json:"ssh_key_ids"`

	// User: user used for the installation.
	User *string `json:"user,omitempty"`

	// Password: password used for the installation.
	Password *string `json:"password,omitempty"`

	// ServiceUser: user used for the service to install.
	ServiceUser *string `json:"service_user,omitempty"`

	// ServicePassword: password used for the service to install.
	ServicePassword *string `json:"service_password,omitempty"`
}

// ListOSRequest: list os request.
type ListOSRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// Page: page number.
	Page *int32 `json:"-"`

	// PageSize: number of OS per page.
	PageSize *uint32 `json:"-"`

	// OfferID: offer IDs to filter OSes for.
	OfferID *string `json:"-"`
}

// ListOSResponse: list os response.
type ListOSResponse struct {
	// TotalCount: total count of matching OS.
	TotalCount uint32 `json:"total_count"`

	// Os: oS that match filters.
	Os []*OS `json:"os"`
}

// UnsafeGetTotalCount should not be used
// Internal usage only
func (r *ListOSResponse) UnsafeGetTotalCount() uint32 {
	return r.TotalCount
}

// UnsafeAppend should not be used
// Internal usage only
func (r *ListOSResponse) UnsafeAppend(res interface{}) (uint32, error) {
	results, ok := res.(*ListOSResponse)
	if !ok {
		return 0, errors.New("%T type cannot be appended to type %T", res, r)
	}

	r.Os = append(r.Os, results.Os...)
	r.TotalCount += uint32(len(results.Os))
	return uint32(len(results.Os)), nil
}

// ListOffersRequest: list offers request.
type ListOffersRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// Page: page number.
	Page *int32 `json:"-"`

	// PageSize: number of offers per page.
	PageSize *uint32 `json:"-"`

	// SubscriptionPeriod: subscription period type to filter offers by.
	// Default value: unknown_subscription_period
	SubscriptionPeriod OfferSubscriptionPeriod `json:"-"`

	// Name: offer name to filter offers by.
	Name *string `json:"-"`
}

// ListOffersResponse: list offers response.
type ListOffersResponse struct {
	// TotalCount: total count of matching offers.
	TotalCount uint32 `json:"total_count"`

	// Offers: offers that match filters.
	Offers []*Offer `json:"offers"`
}

// UnsafeGetTotalCount should not be used
// Internal usage only
func (r *ListOffersResponse) UnsafeGetTotalCount() uint32 {
	return r.TotalCount
}

// UnsafeAppend should not be used
// Internal usage only
func (r *ListOffersResponse) UnsafeAppend(res interface{}) (uint32, error) {
	results, ok := res.(*ListOffersResponse)
	if !ok {
		return 0, errors.New("%T type cannot be appended to type %T", res, r)
	}

	r.Offers = append(r.Offers, results.Offers...)
	r.TotalCount += uint32(len(results.Offers))
	return uint32(len(results.Offers)), nil
}

// ListOptionsRequest: list options request.
type ListOptionsRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// Page: page number.
	Page *int32 `json:"-"`

	// PageSize: number of options per page.
	PageSize *uint32 `json:"-"`

	// OfferID: offer ID to filter options for.
	OfferID *string `json:"-"`

	// Name: name to filter options for.
	Name *string `json:"-"`
}

// ListOptionsResponse: list options response.
type ListOptionsResponse struct {
	// TotalCount: total count of matching options.
	TotalCount uint32 `json:"total_count"`

	// Options: options that match filters.
	Options []*Option `json:"options"`
}

// UnsafeGetTotalCount should not be used
// Internal usage only
func (r *ListOptionsResponse) UnsafeGetTotalCount() uint32 {
	return r.TotalCount
}

// UnsafeAppend should not be used
// Internal usage only
func (r *ListOptionsResponse) UnsafeAppend(res interface{}) (uint32, error) {
	results, ok := res.(*ListOptionsResponse)
	if !ok {
		return 0, errors.New("%T type cannot be appended to type %T", res, r)
	}

	r.Options = append(r.Options, results.Options...)
	r.TotalCount += uint32(len(results.Options))
	return uint32(len(results.Options)), nil
}

// ListServerEventsRequest: list server events request.
type ListServerEventsRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server events searched.
	ServerID string `json:"-"`

	// Page: page number.
	Page *int32 `json:"-"`

	// PageSize: number of server events per page.
	PageSize *uint32 `json:"-"`

	// OrderBy: order of the server events.
	// Default value: created_at_asc
	OrderBy ListServerEventsRequestOrderBy `json:"-"`
}

// ListServerEventsResponse: list server events response.
type ListServerEventsResponse struct {
	// TotalCount: total count of matching events.
	TotalCount uint32 `json:"total_count"`

	// Events: server events that match filters.
	Events []*ServerEvent `json:"events"`
}

// UnsafeGetTotalCount should not be used
// Internal usage only
func (r *ListServerEventsResponse) UnsafeGetTotalCount() uint32 {
	return r.TotalCount
}

// UnsafeAppend should not be used
// Internal usage only
func (r *ListServerEventsResponse) UnsafeAppend(res interface{}) (uint32, error) {
	results, ok := res.(*ListServerEventsResponse)
	if !ok {
		return 0, errors.New("%T type cannot be appended to type %T", res, r)
	}

	r.Events = append(r.Events, results.Events...)
	r.TotalCount += uint32(len(results.Events))
	return uint32(len(results.Events)), nil
}

// ListServerPrivateNetworksResponse: list server private networks response.
type ListServerPrivateNetworksResponse struct {
	ServerPrivateNetworks []*ServerPrivateNetwork `json:"server_private_networks"`

	TotalCount uint32 `json:"total_count"`
}

// UnsafeGetTotalCount should not be used
// Internal usage only
func (r *ListServerPrivateNetworksResponse) UnsafeGetTotalCount() uint32 {
	return r.TotalCount
}

// UnsafeAppend should not be used
// Internal usage only
func (r *ListServerPrivateNetworksResponse) UnsafeAppend(res interface{}) (uint32, error) {
	results, ok := res.(*ListServerPrivateNetworksResponse)
	if !ok {
		return 0, errors.New("%T type cannot be appended to type %T", res, r)
	}

	r.ServerPrivateNetworks = append(r.ServerPrivateNetworks, results.ServerPrivateNetworks...)
	r.TotalCount += uint32(len(results.ServerPrivateNetworks))
	return uint32(len(results.ServerPrivateNetworks)), nil
}

// ListServersRequest: list servers request.
type ListServersRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// Page: page number.
	Page *int32 `json:"-"`

	// PageSize: number of servers per page.
	PageSize *uint32 `json:"-"`

	// OrderBy: order of the servers.
	// Default value: created_at_asc
	OrderBy ListServersRequestOrderBy `json:"-"`

	// Tags: tags to filter for.
	Tags []string `json:"-"`

	// Status: status to filter for.
	Status []string `json:"-"`

	// Name: names to filter for.
	Name *string `json:"-"`

	// OrganizationID: organization ID to filter for.
	OrganizationID *string `json:"-"`

	// ProjectID: project ID to filter for.
	ProjectID *string `json:"-"`

	// OptionID: option ID to filter for.
	OptionID *string `json:"-"`
}

// ListServersResponse: list servers response.
type ListServersResponse struct {
	// TotalCount: total count of matching servers.
	TotalCount uint32 `json:"total_count"`

	// Servers: array of Elastic Metal server objects matching the filters in the request.
	Servers []*Server `json:"servers"`
}

// UnsafeGetTotalCount should not be used
// Internal usage only
func (r *ListServersResponse) UnsafeGetTotalCount() uint32 {
	return r.TotalCount
}

// UnsafeAppend should not be used
// Internal usage only
func (r *ListServersResponse) UnsafeAppend(res interface{}) (uint32, error) {
	results, ok := res.(*ListServersResponse)
	if !ok {
		return 0, errors.New("%T type cannot be appended to type %T", res, r)
	}

	r.Servers = append(r.Servers, results.Servers...)
	r.TotalCount += uint32(len(results.Servers))
	return uint32(len(results.Servers)), nil
}

// ListSettingsRequest: list settings request.
type ListSettingsRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// Page: page number.
	Page *int32 `json:"-"`

	// PageSize: set the maximum list size.
	PageSize *uint32 `json:"-"`

	// OrderBy: sort order for items in the response.
	// Default value: created_at_asc
	OrderBy ListSettingsRequestOrderBy `json:"-"`

	// ProjectID: ID of the Project.
	ProjectID *string `json:"-"`
}

// ListSettingsResponse: list settings response.
type ListSettingsResponse struct {
	// TotalCount: total count of matching settings.
	TotalCount uint32 `json:"total_count"`

	// Settings: settings that match filters.
	Settings []*Setting `json:"settings"`
}

// UnsafeGetTotalCount should not be used
// Internal usage only
func (r *ListSettingsResponse) UnsafeGetTotalCount() uint32 {
	return r.TotalCount
}

// UnsafeAppend should not be used
// Internal usage only
func (r *ListSettingsResponse) UnsafeAppend(res interface{}) (uint32, error) {
	results, ok := res.(*ListSettingsResponse)
	if !ok {
		return 0, errors.New("%T type cannot be appended to type %T", res, r)
	}

	r.Settings = append(r.Settings, results.Settings...)
	r.TotalCount += uint32(len(results.Settings))
	return uint32(len(results.Settings)), nil
}

// PrivateNetworkAPIAddServerPrivateNetworkRequest: private network api add server private network request.
type PrivateNetworkAPIAddServerPrivateNetworkRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: the ID of the server.
	ServerID string `json:"-"`

	// PrivateNetworkID: the ID of the Private Network.
	PrivateNetworkID string `json:"private_network_id"`
}

// PrivateNetworkAPIDeleteServerPrivateNetworkRequest: private network api delete server private network request.
type PrivateNetworkAPIDeleteServerPrivateNetworkRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: the ID of the server.
	ServerID string `json:"-"`

	// PrivateNetworkID: the ID of the Private Network.
	PrivateNetworkID string `json:"-"`
}

// PrivateNetworkAPIListServerPrivateNetworksRequest: private network api list server private networks request.
type PrivateNetworkAPIListServerPrivateNetworksRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// OrderBy: the sort order for the returned Private Networks.
	// Default value: created_at_asc
	OrderBy ListServerPrivateNetworksRequestOrderBy `json:"-"`

	// Page: the page number for the returned Private Networks.
	Page *int32 `json:"-"`

	// PageSize: the maximum number of Private Networks per page.
	PageSize *uint32 `json:"-"`

	// ServerID: filter Private Networks by server ID.
	ServerID *string `json:"-"`

	// PrivateNetworkID: filter Private Networks by Private Network ID.
	PrivateNetworkID *string `json:"-"`

	// OrganizationID: filter Private Networks by Organization ID.
	OrganizationID *string `json:"-"`

	// ProjectID: filter Private Networks by Project ID.
	ProjectID *string `json:"-"`
}

// PrivateNetworkAPISetServerPrivateNetworksRequest: private network api set server private networks request.
type PrivateNetworkAPISetServerPrivateNetworksRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: the ID of the server.
	ServerID string `json:"-"`

	// PrivateNetworkIDs: the IDs of the Private Networks.
	PrivateNetworkIDs []string `json:"private_network_ids"`
}

// RebootServerRequest: reboot server request.
type RebootServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server to reboot.
	ServerID string `json:"-"`

	// BootType: the type of boot.
	// Default value: unknown_boot_type
	BootType ServerBootType `json:"boot_type"`
}

// SetServerPrivateNetworksResponse: set server private networks response.
type SetServerPrivateNetworksResponse struct {
	ServerPrivateNetworks []*ServerPrivateNetwork `json:"server_private_networks"`
}

// StartBMCAccessRequest: start bmc access request.
type StartBMCAccessRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server.
	ServerID string `json:"-"`

	// IP: the IP authorized to connect to the server.
	IP net.IP `json:"ip"`
}

// StartServerRequest: start server request.
type StartServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server to start.
	ServerID string `json:"-"`

	// BootType: the type of boot.
	// Default value: unknown_boot_type
	BootType ServerBootType `json:"boot_type"`
}

// StopBMCAccessRequest: stop bmc access request.
type StopBMCAccessRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server.
	ServerID string `json:"-"`
}

// StopServerRequest: stop server request.
type StopServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server to stop.
	ServerID string `json:"-"`
}

// UpdateIPRequest: update ip request.
type UpdateIPRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server.
	ServerID string `json:"-"`

	// IPID: ID of the IP to update.
	IPID string `json:"-"`

	// Reverse: new reverse IP to update, not updated if null.
	Reverse *string `json:"reverse,omitempty"`
}

// UpdateServerRequest: update server request.
type UpdateServerRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// ServerID: ID of the server to update.
	ServerID string `json:"-"`

	// Name: name of the server (≠hostname), not updated if null.
	Name *string `json:"name,omitempty"`

	// Description: description associated with the server, max 255 characters, not updated if null.
	Description *string `json:"description,omitempty"`

	// Tags: tags associated with the server, not updated if null.
	Tags *[]string `json:"tags,omitempty"`
}

// UpdateSettingRequest: update setting request.
type UpdateSettingRequest struct {
	// Zone: zone to target. If none is passed will use default zone from the config.
	Zone scw.Zone `json:"-"`

	// SettingID: ID of the setting.
	SettingID string `json:"-"`

	// Enabled: defines whether the setting is enabled.
	Enabled *bool `json:"enabled,omitempty"`
}

// This API allows you to manage your Elastic Metal servers.
type API struct {
	client *scw.Client
}

// NewAPI returns a API object from a Scaleway client.
func NewAPI(client *scw.Client) *API {
	return &API{
		client: client,
	}
}
func (s *API) Zones() []scw.Zone {
	return []scw.Zone{scw.ZoneFrPar1, scw.ZoneFrPar2, scw.ZoneNlAms1, scw.ZoneNlAms2}
}

// ListServers: List Elastic Metal servers for a specific Organization.
func (s *API) ListServers(req *ListServersRequest, opts ...scw.RequestOption) (*ListServersResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	defaultPageSize, exist := s.client.GetDefaultPageSize()
	if (req.PageSize == nil || *req.PageSize == 0) && exist {
		req.PageSize = &defaultPageSize
	}

	query := url.Values{}
	parameter.AddToQuery(query, "page", req.Page)
	parameter.AddToQuery(query, "page_size", req.PageSize)
	parameter.AddToQuery(query, "order_by", req.OrderBy)
	parameter.AddToQuery(query, "tags", req.Tags)
	parameter.AddToQuery(query, "status", req.Status)
	parameter.AddToQuery(query, "name", req.Name)
	parameter.AddToQuery(query, "organization_id", req.OrganizationID)
	parameter.AddToQuery(query, "project_id", req.ProjectID)
	parameter.AddToQuery(query, "option_id", req.OptionID)

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers",
		Query:  query,
	}

	var resp ListServersResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetServer: Get full details of an existing Elastic Metal server associated with the ID.
func (s *API) GetServer(req *GetServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "",
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateServer: Create a new Elastic Metal server. Once the server is created, proceed with the [installation of an OS](#post-3e949e).
func (s *API) CreateServer(req *CreateServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	defaultProjectID, exist := s.client.GetDefaultProjectID()
	if exist && req.ProjectID == nil && req.OrganizationID == nil {
		req.ProjectID = &defaultProjectID
	}

	defaultOrganizationID, exist := s.client.GetDefaultOrganizationID()
	if exist && req.ProjectID == nil && req.OrganizationID == nil {
		req.OrganizationID = &defaultOrganizationID
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "POST",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateServer: Update the server associated with the ID. You can update parameters such as the server's name, tags and description. Any parameters left null in the request body are not updated.
func (s *API) UpdateServer(req *UpdateServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "PATCH",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// InstallServer: Install an Operating System (OS) on the Elastic Metal server with a specific ID.
func (s *API) InstallServer(req *InstallServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "POST",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/install",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetServerMetrics: Get the ping status of the server associated with the ID.
func (s *API) GetServerMetrics(req *GetServerMetricsRequest, opts ...scw.RequestOption) (*GetServerMetricsResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/metrics",
	}

	var resp GetServerMetricsResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteServer: Delete the server associated with the ID.
func (s *API) DeleteServer(req *DeleteServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "DELETE",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "",
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// RebootServer: Reboot the Elastic Metal server associated with the ID, use the `boot_type` `rescue` to reboot the server in rescue mode.
func (s *API) RebootServer(req *RebootServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "POST",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/reboot",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// StartServer: Start the server associated with the ID.
func (s *API) StartServer(req *StartServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "POST",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/start",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// StopServer: Stop the server associated with the ID. The server remains allocated to your account and all data remains on the local storage of the server.
func (s *API) StopServer(req *StopServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "POST",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/stop",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListServerEvents: List event (i.e. start/stop/reboot) associated to the server ID.
func (s *API) ListServerEvents(req *ListServerEventsRequest, opts ...scw.RequestOption) (*ListServerEventsResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	defaultPageSize, exist := s.client.GetDefaultPageSize()
	if (req.PageSize == nil || *req.PageSize == 0) && exist {
		req.PageSize = &defaultPageSize
	}

	query := url.Values{}
	parameter.AddToQuery(query, "page", req.Page)
	parameter.AddToQuery(query, "page_size", req.PageSize)
	parameter.AddToQuery(query, "order_by", req.OrderBy)

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/events",
		Query:  query,
	}

	var resp ListServerEventsResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// StartBMCAccess: Start BMC (Baseboard Management Controller) access associated with the ID.
// The BMC (Baseboard Management Controller) access is available one hour after the installation of the server.
// You need first to create an option Remote Access. You will find the ID and the price with a call to listOffers (https://developers.scaleway.com/en/products/baremetal/api/#get-78db92). Then add the option https://developers.scaleway.com/en/products/baremetal/api/#post-b14abd.
// After adding the BMC option, you need to Get Remote Access to get the login/password https://developers.scaleway.com/en/products/baremetal/api/#get-cefc0f. Do not forget to delete the Option after use.
func (s *API) StartBMCAccess(req *StartBMCAccessRequest, opts ...scw.RequestOption) (*BMCAccess, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "POST",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/bmc-access",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp BMCAccess

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetBMCAccess: Get the BMC (Baseboard Management Controller) access associated with the ID, including the URL and login information needed to connect.
func (s *API) GetBMCAccess(req *GetBMCAccessRequest, opts ...scw.RequestOption) (*BMCAccess, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/bmc-access",
	}

	var resp BMCAccess

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// StopBMCAccess: Stop BMC (Baseboard Management Controller) access associated with the ID.
func (s *API) StopBMCAccess(req *StopBMCAccessRequest, opts ...scw.RequestOption) error {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "DELETE",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/bmc-access",
	}

	err = s.client.Do(scwReq, nil, opts...)
	if err != nil {
		return err
	}
	return nil
}

// UpdateIP: Configure the IP address associated with the server ID and IP ID. You can use this method to set a reverse DNS for an IP address.
func (s *API) UpdateIP(req *UpdateIPRequest, opts ...scw.RequestOption) (*IP, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	if fmt.Sprint(req.IPID) == "" {
		return nil, errors.New("field IPID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "PATCH",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/ips/" + fmt.Sprint(req.IPID) + "",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp IP

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// AddOptionServer: Add an option, such as Private Networks, to a specific server.
func (s *API) AddOptionServer(req *AddOptionServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	if fmt.Sprint(req.OptionID) == "" {
		return nil, errors.New("field OptionID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "POST",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/options/" + fmt.Sprint(req.OptionID) + "",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteOptionServer: Delete an option from a specific server.
func (s *API) DeleteOptionServer(req *DeleteOptionServerRequest, opts ...scw.RequestOption) (*Server, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	if fmt.Sprint(req.OptionID) == "" {
		return nil, errors.New("field OptionID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "DELETE",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/options/" + fmt.Sprint(req.OptionID) + "",
	}

	var resp Server

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListOffers: List all available Elastic Metal server configurations.
func (s *API) ListOffers(req *ListOffersRequest, opts ...scw.RequestOption) (*ListOffersResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	defaultPageSize, exist := s.client.GetDefaultPageSize()
	if (req.PageSize == nil || *req.PageSize == 0) && exist {
		req.PageSize = &defaultPageSize
	}

	query := url.Values{}
	parameter.AddToQuery(query, "page", req.Page)
	parameter.AddToQuery(query, "page_size", req.PageSize)
	parameter.AddToQuery(query, "subscription_period", req.SubscriptionPeriod)
	parameter.AddToQuery(query, "name", req.Name)

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/offers",
		Query:  query,
	}

	var resp ListOffersResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetOffer: Get details of an offer identified by its offer ID.
func (s *API) GetOffer(req *GetOfferRequest, opts ...scw.RequestOption) (*Offer, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.OfferID) == "" {
		return nil, errors.New("field OfferID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/offers/" + fmt.Sprint(req.OfferID) + "",
	}

	var resp Offer

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetOption: Return specific option for the ID.
func (s *API) GetOption(req *GetOptionRequest, opts ...scw.RequestOption) (*Option, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.OptionID) == "" {
		return nil, errors.New("field OptionID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/options/" + fmt.Sprint(req.OptionID) + "",
	}

	var resp Option

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListOptions: List all options matching with filters.
func (s *API) ListOptions(req *ListOptionsRequest, opts ...scw.RequestOption) (*ListOptionsResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	defaultPageSize, exist := s.client.GetDefaultPageSize()
	if (req.PageSize == nil || *req.PageSize == 0) && exist {
		req.PageSize = &defaultPageSize
	}

	query := url.Values{}
	parameter.AddToQuery(query, "page", req.Page)
	parameter.AddToQuery(query, "page_size", req.PageSize)
	parameter.AddToQuery(query, "offer_id", req.OfferID)
	parameter.AddToQuery(query, "name", req.Name)

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/options",
		Query:  query,
	}

	var resp ListOptionsResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListSettings: Return all settings for a Project ID.
func (s *API) ListSettings(req *ListSettingsRequest, opts ...scw.RequestOption) (*ListSettingsResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	defaultPageSize, exist := s.client.GetDefaultPageSize()
	if (req.PageSize == nil || *req.PageSize == 0) && exist {
		req.PageSize = &defaultPageSize
	}

	query := url.Values{}
	parameter.AddToQuery(query, "page", req.Page)
	parameter.AddToQuery(query, "page_size", req.PageSize)
	parameter.AddToQuery(query, "order_by", req.OrderBy)
	parameter.AddToQuery(query, "project_id", req.ProjectID)

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/settings",
		Query:  query,
	}

	var resp ListSettingsResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateSetting: Update a setting for a Project ID (enable or disable).
func (s *API) UpdateSetting(req *UpdateSettingRequest, opts ...scw.RequestOption) (*Setting, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.SettingID) == "" {
		return nil, errors.New("field SettingID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "PATCH",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/settings/" + fmt.Sprint(req.SettingID) + "",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp Setting

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListOS: List all OSes that are available for installation on Elastic Metal servers.
func (s *API) ListOS(req *ListOSRequest, opts ...scw.RequestOption) (*ListOSResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	defaultPageSize, exist := s.client.GetDefaultPageSize()
	if (req.PageSize == nil || *req.PageSize == 0) && exist {
		req.PageSize = &defaultPageSize
	}

	query := url.Values{}
	parameter.AddToQuery(query, "page", req.Page)
	parameter.AddToQuery(query, "page_size", req.PageSize)
	parameter.AddToQuery(query, "offer_id", req.OfferID)

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/os",
		Query:  query,
	}

	var resp ListOSResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetOS: Return the specific OS for the ID.
func (s *API) GetOS(req *GetOSRequest, opts ...scw.RequestOption) (*OS, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.OsID) == "" {
		return nil, errors.New("field OsID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/os/" + fmt.Sprint(req.OsID) + "",
	}

	var resp OS

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Elastic Metal - Private Network API.
type PrivateNetworkAPI struct {
	client *scw.Client
}

// NewPrivateNetworkAPI returns a PrivateNetworkAPI object from a Scaleway client.
func NewPrivateNetworkAPI(client *scw.Client) *PrivateNetworkAPI {
	return &PrivateNetworkAPI{
		client: client,
	}
}
func (s *PrivateNetworkAPI) Zones() []scw.Zone {
	return []scw.Zone{scw.ZoneFrPar2}
}

// AddServerPrivateNetwork: Add a server to a Private Network.
func (s *PrivateNetworkAPI) AddServerPrivateNetwork(req *PrivateNetworkAPIAddServerPrivateNetworkRequest, opts ...scw.RequestOption) (*ServerPrivateNetwork, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "POST",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/private-networks",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp ServerPrivateNetwork

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetServerPrivateNetworks: Set multiple Private Networks on a server.
func (s *PrivateNetworkAPI) SetServerPrivateNetworks(req *PrivateNetworkAPISetServerPrivateNetworksRequest, opts ...scw.RequestOption) (*SetServerPrivateNetworksResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return nil, errors.New("field ServerID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "PUT",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/private-networks",
	}

	err = scwReq.SetBody(req)
	if err != nil {
		return nil, err
	}

	var resp SetServerPrivateNetworksResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListServerPrivateNetworks: List the Private Networks of a server.
func (s *PrivateNetworkAPI) ListServerPrivateNetworks(req *PrivateNetworkAPIListServerPrivateNetworksRequest, opts ...scw.RequestOption) (*ListServerPrivateNetworksResponse, error) {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	defaultPageSize, exist := s.client.GetDefaultPageSize()
	if (req.PageSize == nil || *req.PageSize == 0) && exist {
		req.PageSize = &defaultPageSize
	}

	query := url.Values{}
	parameter.AddToQuery(query, "order_by", req.OrderBy)
	parameter.AddToQuery(query, "page", req.Page)
	parameter.AddToQuery(query, "page_size", req.PageSize)
	parameter.AddToQuery(query, "server_id", req.ServerID)
	parameter.AddToQuery(query, "private_network_id", req.PrivateNetworkID)
	parameter.AddToQuery(query, "organization_id", req.OrganizationID)
	parameter.AddToQuery(query, "project_id", req.ProjectID)

	if fmt.Sprint(req.Zone) == "" {
		return nil, errors.New("field Zone cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "GET",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/server-private-networks",
		Query:  query,
	}

	var resp ListServerPrivateNetworksResponse

	err = s.client.Do(scwReq, &resp, opts...)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteServerPrivateNetwork: Delete a Private Network.
func (s *PrivateNetworkAPI) DeleteServerPrivateNetwork(req *PrivateNetworkAPIDeleteServerPrivateNetworkRequest, opts ...scw.RequestOption) error {
	var err error

	if req.Zone == "" {
		defaultZone, _ := s.client.GetDefaultZone()
		req.Zone = defaultZone
	}

	if fmt.Sprint(req.Zone) == "" {
		return errors.New("field Zone cannot be empty in request")
	}

	if fmt.Sprint(req.ServerID) == "" {
		return errors.New("field ServerID cannot be empty in request")
	}

	if fmt.Sprint(req.PrivateNetworkID) == "" {
		return errors.New("field PrivateNetworkID cannot be empty in request")
	}

	scwReq := &scw.ScalewayRequest{
		Method: "DELETE",
		Path:   "/baremetal/v1/zones/" + fmt.Sprint(req.Zone) + "/servers/" + fmt.Sprint(req.ServerID) + "/private-networks/" + fmt.Sprint(req.PrivateNetworkID) + "",
	}

	err = s.client.Do(scwReq, nil, opts...)
	if err != nil {
		return err
	}
	return nil
}
