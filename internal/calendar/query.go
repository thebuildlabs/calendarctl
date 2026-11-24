package calendar

// QueryOptions are the user-facing filters accepted by `event list`.
type QueryOptions struct {
	Today    bool
	Tomorrow bool
	Week     bool
	Month    bool
	From     string
	To       string
	Calendar string
	Search   string
	Limit    int
}
