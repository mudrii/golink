package output

import (
	"strconv"
	"time"
)

// CommandStatus describes the top-level result class for a command or tool.
type CommandStatus string

const (
	// StatusOK indicates a successful command result.
	StatusOK CommandStatus = "ok"
	// StatusUnsupported indicates the requested feature is not available.
	StatusUnsupported CommandStatus = "unsupported"
	// StatusError indicates command execution failed.
	StatusError CommandStatus = "error"
	// StatusValidation indicates argument or usage validation failed.
	StatusValidation CommandStatus = "validation_error"
)

// ErrorCode classifies machine-readable command failures.
type ErrorCode string

const (
	// ErrorCodeUnauthorized indicates the session is missing or invalid.
	ErrorCodeUnauthorized ErrorCode = "UNAUTHORIZED"
	// ErrorCodeForbidden indicates the caller lacks permission for the operation.
	ErrorCodeForbidden ErrorCode = "FORBIDDEN"
	// ErrorCodeNotFound indicates the requested resource was not found.
	ErrorCodeNotFound ErrorCode = "NOT_FOUND"
	// ErrorCodeConflict indicates the request conflicts with current state.
	ErrorCodeConflict ErrorCode = "CONFLICT"
	// ErrorCodeRateLimited indicates retries were exhausted after rate limiting.
	ErrorCodeRateLimited ErrorCode = "RATE_LIMITED"
	// ErrorCodeUnavailable indicates the upstream API is temporarily unavailable.
	ErrorCodeUnavailable ErrorCode = "UNAVAILABLE"
	// ErrorCodeValidation indicates the request failed local validation.
	ErrorCodeValidation ErrorCode = "VALIDATION_ERROR"
	// ErrorCodeTransport indicates a transport-level failure.
	ErrorCodeTransport ErrorCode = "TRANSPORT_ERROR"
	// ErrorCodeUnsupported indicates the feature is not supported in the current mode.
	ErrorCodeUnsupported ErrorCode = "UNSUPPORTED"
)

// BaseEnvelope contains fields shared by every JSON response shape.
type BaseEnvelope struct {
	Status      CommandStatus  `json:"status"`
	CommandID   string         `json:"command_id"`
	Command     string         `json:"command"`
	Transport   string         `json:"transport"`
	Mode        string         `json:"mode,omitempty"`
	RequestID   string         `json:"request_id,omitempty"`
	GeneratedAt time.Time      `json:"generated_at"`
	RateLimit   *RateLimitInfo `json:"rate_limit,omitempty"`
}

// SuccessEnvelope wraps successful command data.
type SuccessEnvelope[D any] struct {
	BaseEnvelope
	Data D `json:"data"`
}

// ErrorEnvelope is the canonical command error shape.
type ErrorEnvelope struct {
	BaseEnvelope
	Error   string    `json:"error"`
	Code    ErrorCode `json:"code"`
	Details string    `json:"details,omitempty"`
}

// ValidationErrorEnvelope is the canonical validation failure shape.
type ValidationErrorEnvelope struct {
	BaseEnvelope
	Error   string    `json:"error"`
	Code    ErrorCode `json:"code"`
	Details string    `json:"details,omitempty"`
}

// UnsupportedPayload describes an unsupported feature result.
type UnsupportedPayload struct {
	Feature           string `json:"feature"`
	Reason            string `json:"reason"`
	SuggestedFallback string `json:"suggested_fallback,omitempty"`
}

// Locale describes the user's locale.
type Locale struct {
	Country  string `json:"country"`
	Language string `json:"language"`
}

// RateLimitInfo carries parsed rate limit metadata when available.
type RateLimitInfo struct {
	Remaining *int   `json:"remaining,omitempty"`
	ResetAt   string `json:"reset_at,omitempty"`
}

// AuthStatusData is returned by auth status.
type AuthStatusData struct {
	IsAuthenticated  bool     `json:"is_authenticated"`
	Profile          string   `json:"profile"`
	Transport        string   `json:"transport"`
	Scopes           []string `json:"scopes,omitempty"`
	ExpiresAt        string   `json:"expires_at,omitempty"`
	RefreshExpiresAt string   `json:"refresh_expires_at,omitempty"`
	AuthFlow         string   `json:"auth_flow,omitempty"`
}

// AuthRefreshData is returned by auth refresh.
type AuthRefreshData struct {
	Profile          string   `json:"profile"`
	Transport        string   `json:"transport"`
	RefreshedAt      string   `json:"refreshed_at"`
	ExpiresAt        string   `json:"expires_at"`
	RefreshExpiresAt string   `json:"refresh_expires_at,omitempty"`
	ScopesGranted    []string `json:"scopes_granted,omitempty"`
}

// AuthLoginData contains the login URL and flow metadata.
type AuthLoginData struct {
	URL       string `json:"url"`
	Profile   string `json:"profile"`
	Transport string `json:"transport"`
	TimeoutMs int    `json:"timeout_ms"`
}

// AuthLoginResultData contains the terminal login result.
type AuthLoginResultData struct {
	Status        string   `json:"status"`
	Profile       string   `json:"profile"`
	Transport     string   `json:"transport"`
	ConnectedAt   string   `json:"connected_at"`
	ScopesGranted []string `json:"scopes_granted,omitempty"`
}

// AuthLogoutData contains logout state.
type AuthLogoutData struct {
	Status    string `json:"status"`
	Profile   string `json:"profile"`
	Transport string `json:"transport"`
	Cleared   bool   `json:"cleared"`
}

// ProfileData contains the authenticated profile summary.
type ProfileData struct {
	Sub       string `json:"sub"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	Picture   string `json:"picture,omitempty"`
	Locale    Locale `json:"locale"`
	ProfileID string `json:"profile_id,omitempty"`
}

// Visibility is the LinkedIn post visibility.
type Visibility string

const (
	// VisibilityPublic makes a post public.
	VisibilityPublic Visibility = "PUBLIC"
	// VisibilityConnections restricts a post to connections.
	VisibilityConnections Visibility = "CONNECTIONS"
	// VisibilityLoggedIn restricts a post to logged-in members.
	VisibilityLoggedIn Visibility = "LOGGED_IN"
)

// PostPayloadPreview describes a dry-run post request.
type PostPayloadPreview struct {
	Endpoint   string     `json:"endpoint"`
	Text       string     `json:"text"`
	Visibility Visibility `json:"visibility"`
	Media      string     `json:"media,omitempty"`
}

// PostSummary contains the common post fields used by multiple commands.
type PostSummary struct {
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	Text       string     `json:"text"`
	Visibility Visibility `json:"visibility"`
	URL        string     `json:"url"`
	AuthorURN  string     `json:"author_urn"`
}

// PostListItem contains list-specific post metadata.
type PostListItem struct {
	PostSummary
	LikeCount    int `json:"like_count,omitempty"`
	CommentCount int `json:"comment_count,omitempty"`
}

// PostListData contains paginated post results.
type PostListData struct {
	OwnerURN string         `json:"owner_urn"`
	Count    int            `json:"count"`
	Start    int            `json:"start"`
	Items    []PostListItem `json:"items"`
}

// PostCreateData contains the created post summary.
type PostCreateData struct {
	PostSummary
	Mode string `json:"mode,omitempty"`
}

// PostCreateDryRunData contains the dry-run preview for post create.
type PostCreateDryRunData struct {
	WouldPost PostPayloadPreview `json:"would_post"`
	Mode      string             `json:"mode"`
}

// PostDeletePreview describes a dry-run post delete request.
type PostDeletePreview struct {
	Endpoint string `json:"endpoint"`
	PostURN  string `json:"post_urn"`
}

// PostDeleteDryRunData contains the dry-run preview for post delete.
type PostDeleteDryRunData struct {
	WouldDelete PostDeletePreview `json:"would_delete"`
	Mode        string            `json:"mode"`
}

// CommentAddPreview describes a dry-run comment add request.
type CommentAddPreview struct {
	Endpoint string `json:"endpoint"`
	PostURN  string `json:"post_urn"`
	Text     string `json:"text"`
}

// CommentAddDryRunData contains the dry-run preview for comment add.
type CommentAddDryRunData struct {
	WouldComment CommentAddPreview `json:"would_comment"`
	Mode         string            `json:"mode"`
}

// ReactionAddPreview describes a dry-run reaction add request.
type ReactionAddPreview struct {
	Endpoint string       `json:"endpoint"`
	PostURN  string       `json:"post_urn"`
	Type     ReactionType `json:"type"`
}

// ReactionAddDryRunData contains the dry-run preview for react add.
type ReactionAddDryRunData struct {
	WouldReact ReactionAddPreview `json:"would_react"`
	Mode       string             `json:"mode"`
}

// PostGetData contains details for a single post.
type PostGetData struct {
	PostSummary
	LikeCount    int   `json:"like_count,omitempty"`
	CommentCount int   `json:"comment_count,omitempty"`
	PublishTime  int64 `json:"publish_time,omitempty"`
}

// PostDeleteData contains the deletion outcome.
type PostDeleteData struct {
	ID        string `json:"id"`
	Deleted   bool   `json:"deleted"`
	Revisions int    `json:"revisions,omitempty"`
}

// CommentData contains a LinkedIn comment summary.
type CommentData struct {
	ID        string    `json:"id"`
	PostURN   string    `json:"post_urn"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	Likeable  bool      `json:"likeable,omitempty"`
}

// CommentAddData contains the created comment result.
type CommentAddData struct {
	CommentData
}

// CommentListData contains paginated comments.
type CommentListData struct {
	PostURN string        `json:"post_urn"`
	Items   []CommentData `json:"items"`
	Count   int           `json:"count"`
	Start   int           `json:"start"`
}

// ReactionType is the allowed LinkedIn reaction enum.
type ReactionType string

const (
	// ReactionLike is the LIKE reaction.
	ReactionLike ReactionType = "LIKE"
	// ReactionPraise is the PRAISE reaction.
	ReactionPraise ReactionType = "PRAISE"
	// ReactionEmpathy is the EMPATHY reaction.
	ReactionEmpathy ReactionType = "EMPATHY"
	// ReactionInterest is the INTEREST reaction.
	ReactionInterest ReactionType = "INTEREST"
	// ReactionAppreciation is the APPRECIATION reaction.
	ReactionAppreciation ReactionType = "APPRECIATION"
	// ReactionEntertainment is the ENTERTAINMENT reaction.
	ReactionEntertainment ReactionType = "ENTERTAINMENT"
)

// ReactionData contains a single reaction.
type ReactionData struct {
	PostURN string       `json:"post_urn"`
	Actor   string       `json:"actor_urn"`
	Type    ReactionType `json:"type"`
	At      time.Time    `json:"at"`
}

// ReactionAddData contains the reaction creation result.
type ReactionAddData struct {
	ReactionData
	TargetURN string `json:"target_urn"`
}

// ReactionListData contains reactions for an entity.
type ReactionListData struct {
	PostURN string         `json:"post_urn"`
	Items   []ReactionData `json:"items"`
	Count   int            `json:"count"`
}

// Person contains a search result entry.
type Person struct {
	URN            string   `json:"urn"`
	FullName       string   `json:"full_name"`
	Headline       string   `json:"headline"`
	Location       string   `json:"location"`
	Industry       string   `json:"industry"`
	ProfilePicture string   `json:"profile_picture,omitempty"`
	Skills         []string `json:"skills,omitempty"`
}

// SearchPeopleData contains people search results.
type SearchPeopleData struct {
	Query      string   `json:"query"`
	Count      int      `json:"count"`
	Start      int      `json:"start"`
	TotalCount int      `json:"total_count"`
	People     []Person `json:"people"`
}

// VersionData contains build and runtime metadata.
type VersionData struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
}

// AuthStatusOutput is the schema-aligned auth status envelope.
type AuthStatusOutput = SuccessEnvelope[AuthStatusData]

// AuthRefreshOutput is the schema-aligned auth refresh envelope.
type AuthRefreshOutput = SuccessEnvelope[AuthRefreshData]

// AuthLoginOutput is the schema-aligned auth login envelope.
type AuthLoginOutput = SuccessEnvelope[AuthLoginData]

// AuthLoginResultOutput is the schema-aligned auth login result envelope.
type AuthLoginResultOutput = SuccessEnvelope[AuthLoginResultData]

// AuthLogoutOutput is the schema-aligned auth logout envelope.
type AuthLogoutOutput = SuccessEnvelope[AuthLogoutData]

// ProfileMeOutput is the schema-aligned profile envelope.
type ProfileMeOutput = SuccessEnvelope[ProfileData]

// PostCreateOutput is the schema-aligned post create envelope.
type PostCreateOutput = SuccessEnvelope[PostCreateData]

// PostCreateDryRunOutput is the schema-aligned dry-run envelope.
type PostCreateDryRunOutput = SuccessEnvelope[PostCreateDryRunData]

// PostListOutput is the schema-aligned post list envelope.
type PostListOutput = SuccessEnvelope[PostListData]

// PostGetOutput is the schema-aligned post get envelope.
type PostGetOutput = SuccessEnvelope[PostGetData]

// PostDeleteOutput is the schema-aligned post delete envelope.
type PostDeleteOutput = SuccessEnvelope[PostDeleteData]

// CommentAddOutput is the schema-aligned comment add envelope.
type CommentAddOutput = SuccessEnvelope[CommentAddData]

// CommentListOutput is the schema-aligned comment list envelope.
type CommentListOutput = SuccessEnvelope[CommentListData]

// ReactionAddOutput is the schema-aligned reaction add envelope.
type ReactionAddOutput = SuccessEnvelope[ReactionAddData]

// ReactionListOutput is the schema-aligned reaction list envelope.
type ReactionListOutput = SuccessEnvelope[ReactionListData]

// SearchPeopleOutput is the schema-aligned search output envelope.
type SearchPeopleOutput = SuccessEnvelope[SearchPeopleData]

// UnsupportedOutput is the schema-aligned unsupported envelope.
type UnsupportedOutput = SuccessEnvelope[UnsupportedPayload]

// VersionOutput is the schema-aligned version envelope.
type VersionOutput = SuccessEnvelope[VersionData]

// Headers implements TabularData for PostListData.
func (d PostListData) Headers() []string {
	return []string{"URN", "VISIBILITY", "CREATED", "LIKES", "COMMENTS", "TEXT"}
}

// Rows implements TabularData for PostListData.
func (d PostListData) Rows() [][]string {
	rows := make([][]string, 0, len(d.Items))
	for _, item := range d.Items {
		rows = append(rows, []string{
			item.ID,
			string(item.Visibility),
			item.CreatedAt.UTC().Format(time.DateOnly),
			strconv.Itoa(item.LikeCount),
			strconv.Itoa(item.CommentCount),
			item.Text,
		})
	}
	return rows
}

// Headers implements TabularData for CommentListData.
func (d CommentListData) Headers() []string {
	return []string{"ID", "AUTHOR", "CREATED", "TEXT"}
}

// Rows implements TabularData for CommentListData.
func (d CommentListData) Rows() [][]string {
	rows := make([][]string, 0, len(d.Items))
	for _, item := range d.Items {
		rows = append(rows, []string{
			item.ID,
			item.Author,
			item.CreatedAt.UTC().Format(time.DateOnly),
			item.Text,
		})
	}
	return rows
}

// Headers implements TabularData for ReactionListData.
func (d ReactionListData) Headers() []string {
	return []string{"ACTOR", "TYPE", "AT"}
}

// Rows implements TabularData for ReactionListData.
func (d ReactionListData) Rows() [][]string {
	rows := make([][]string, 0, len(d.Items))
	for _, item := range d.Items {
		rows = append(rows, []string{
			item.Actor,
			string(item.Type),
			item.At.UTC().Format(time.DateOnly),
		})
	}
	return rows
}

// Headers implements TabularData for SearchPeopleData.
func (d SearchPeopleData) Headers() []string {
	return []string{"URN", "NAME", "HEADLINE", "LOCATION"}
}

// Rows implements TabularData for SearchPeopleData.
func (d SearchPeopleData) Rows() [][]string {
	rows := make([][]string, 0, len(d.People))
	for _, p := range d.People {
		rows = append(rows, []string{
			p.URN,
			p.FullName,
			p.Headline,
			p.Location,
		})
	}
	return rows
}
