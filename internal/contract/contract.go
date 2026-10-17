// Package contract owns the exporter and upstream protocol identifiers.
package contract

const (
	FieldStatus          = "status"
	FieldRetcode         = "retcode"
	FieldData            = "data"
	FieldOnline          = "online"
	Action               = "get_status"
	StatusOK             = "ok"
	RetcodeOK            = 0
	Up                   = 1.0
	Down                 = 0.0
	APIUp                = "napcat_api_up"
	Online               = "napcat_online"
	LastSuccess          = "napcat_last_successful_collection_timestamp_seconds"
	Duration             = "napcat_scrape_duration_seconds"
	BuildInfo            = "napcat_exporter_build_info"
	ActionLabel          = "action"
	HealthPath           = "/healthz"
	MaxResponseBytes     = 1 << 20
	MaxConcurrentScrapes = 2
)
