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
	// StatusPendingApproval indicates the command was staged for operator approval.
	StatusPendingApproval CommandStatus = "pending_approval"
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
	FromCache   bool           `json:"from_cache,omitempty"`
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

// MediaPreview describes a media attachment in a post payload.
type MediaPreview struct {
	ImageURN string `json:"image_urn"`
	Title    string `json:"title,omitempty"`
	Alt      string `json:"alt,omitempty"`
}

// ImageUploadPreview describes the upload plan shown in a dry-run image post.
type ImageUploadPreview struct {
	Path           string `json:"path"`
	PlaceholderURN string `json:"placeholder_urn"`
	Alt            string `json:"alt,omitempty"`
}

// PostPayloadPreview describes a dry-run post request.
type PostPayloadPreview struct {
	Endpoint    string              `json:"endpoint"`
	Text        string              `json:"text"`
	Visibility  Visibility          `json:"visibility"`
	Media       string              `json:"media,omitempty"`
	WouldUpload *ImageUploadPreview `json:"would_upload,omitempty"`
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

// SocialMetadataItem holds engagement metrics for a single post URN.
type SocialMetadataItem struct {
	PostURN         string         `json:"post_urn"`
	LikeCount       int            `json:"like_count"`
	CommentCount    int            `json:"comment_count"`
	AllCommentCount int            `json:"all_comment_count,omitempty"`
	ReactionCount   int            `json:"reaction_count"`
	ReactionCounts  map[string]int `json:"reaction_counts,omitempty"`
	CommentsState   string         `json:"comments_state,omitempty"`
	Error           string         `json:"error,omitempty"`
}

// SocialMetadataData is the payload returned by social metadata.
type SocialMetadataData struct {
	Items []SocialMetadataItem `json:"items"`
	Count int                  `json:"count"`
}

// SocialMetadataOutput is the schema-aligned social metadata envelope.
type SocialMetadataOutput = SuccessEnvelope[SocialMetadataData]

// PostEditPreview describes a dry-run post edit (PATCH) request.
type PostEditPreview struct {
	Endpoint string         `json:"endpoint"`
	PostURN  string         `json:"post_urn"`
	Patch    map[string]any `json:"patch,omitempty"`
}

// PostEditData contains the result of an edit (PATCH) on a post.
type PostEditData struct {
	PostSummary
	UpdatedAt time.Time `json:"updated_at,omitempty,omitzero"`
}

// PostEditDryRunData contains the dry-run preview for post edit.
type PostEditDryRunData struct {
	WouldPatch PostEditPreview `json:"would_patch"`
	Mode       string          `json:"mode"`
}

// PostResharePreview describes a dry-run reshare request.
type PostResharePreview struct {
	Endpoint   string     `json:"endpoint"`
	ParentURN  string     `json:"parent_urn"`
	Commentary string     `json:"commentary,omitempty"`
	Visibility Visibility `json:"visibility"`
}

// PostReshareDryRunData contains the dry-run preview for post reshare.
type PostReshareDryRunData struct {
	WouldReshare PostResharePreview `json:"would_reshare"`
	Mode         string             `json:"mode"`
}

// PostEditOutput is the schema-aligned post edit envelope.
type PostEditOutput = SuccessEnvelope[PostEditData]

// PostEditDryRunOutput is the schema-aligned post edit dry-run envelope.
type PostEditDryRunOutput = SuccessEnvelope[PostEditDryRunData]

// PostReshareOutput is the schema-aligned post reshare envelope (reuses PostCreateData).
type PostReshareOutput = SuccessEnvelope[PostCreateData]

// PostReshareDryRunOutput is the schema-aligned post reshare dry-run envelope.
type PostReshareDryRunOutput = SuccessEnvelope[PostReshareDryRunData]

// DoctorEnvironment describes the observed environment variable state.
type DoctorEnvironment struct {
	GOLINKClientID   bool   `json:"golink_client_id_set"`
	GOLINKAPIVersion string `json:"golink_api_version,omitempty"`
	GOLINKRedirect   string `json:"golink_redirect_port,omitempty"`
	GOLINKJSON       string `json:"golink_json,omitempty"`
	GOLINKTransport  string `json:"golink_transport,omitempty"`
	GOLINKOutput     string `json:"golink_output,omitempty"`
	GOLINKAudit      string `json:"golink_audit,omitempty"`
	GOLINKAuditPath  string `json:"golink_audit_path,omitempty"`
	ConfigPath       string `json:"config_path,omitempty"`
	ConfigLoaded     bool   `json:"config_loaded"`
}

// DoctorSession describes the active session state.
type DoctorSession struct {
	Profile          string   `json:"profile"`
	Authenticated    bool     `json:"authenticated"`
	ExpiresAt        string   `json:"expires_at,omitempty"`
	ExpiresInHours   int      `json:"expires_in_hours,omitempty"`
	RefreshAvailable bool     `json:"refresh_available"`
	RefreshExpiresAt string   `json:"refresh_expires_at,omitempty"`
	RefreshInDays    int      `json:"refresh_in_days,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	AuthFlow         string   `json:"auth_flow,omitempty"`
	ConnectedAt      string   `json:"connected_at,omitempty"`
}

// DoctorProbe holds the result of the /v2/userinfo connectivity check.
type DoctorProbe struct {
	URL       string `json:"url"`
	Status    int    `json:"status,omitempty"`
	Member    string `json:"member_urn,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Error     string `json:"error,omitempty"`
	Attempted bool   `json:"attempted"`
}

// DoctorFeature reports whether a command family is available given the
// current scopes and transport.
type DoctorFeature struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

// DoctorAudit describes the audit log configuration and file state.
type DoctorAudit struct {
	Path       string `json:"path"`
	Enabled    bool   `json:"enabled"`
	Exists     bool   `json:"exists"`
	Size       int64  `json:"size,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

// DoctorData is the payload for the doctor command.
type DoctorData struct {
	APIVersion  string            `json:"api_version,omitempty"`
	Environment DoctorEnvironment `json:"environment"`
	Session     DoctorSession     `json:"session"`
	Probe       DoctorProbe       `json:"probe"`
	Features    []DoctorFeature   `json:"features"`
	Audit       DoctorAudit       `json:"audit"`
	Warnings    []string          `json:"warnings,omitempty"`
	Errors      []string          `json:"errors,omitempty"`
	Health      string            `json:"health"`
}

// DoctorOutput is the schema-aligned doctor envelope.
type DoctorOutput = SuccessEnvelope[DoctorData]

// ApprovalPendingData is returned when --require-approval stages a request.
type ApprovalPendingData struct {
	CommandID      string    `json:"command_id"`
	Command        string    `json:"command"`
	StagedAt       time.Time `json:"staged_at"`
	StagedPath     string    `json:"staged_path"`
	Payload        any       `json:"payload"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
}

// ApprovalListItem is one row from approval list.
type ApprovalListItem struct {
	CommandID      string    `json:"command_id"`
	Command        string    `json:"command"`
	State          string    `json:"state"`
	StagedAt       time.Time `json:"staged_at"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
}

// ApprovalListData is the payload for approval list.
type ApprovalListData struct {
	Items []ApprovalListItem `json:"items"`
}

// ApprovalShowData is the payload for approval show.
type ApprovalShowData struct {
	Entry any    `json:"entry"`
	State string `json:"state"`
}

// ApprovalStateChangeData is returned by grant/deny/cancel/run.
type ApprovalStateChangeData struct {
	CommandID string `json:"command_id"`
	State     string `json:"state"`
}

// ApprovalPendingOutput is the schema-aligned pending_approval envelope.
type ApprovalPendingOutput struct {
	BaseEnvelope
	Data ApprovalPendingData `json:"data"`
}

// ApprovalListOutput is the schema-aligned approval list envelope.
type ApprovalListOutput = SuccessEnvelope[ApprovalListData]

// ApprovalShowOutput is the schema-aligned approval show envelope.
type ApprovalShowOutput = SuccessEnvelope[ApprovalShowData]

// ApprovalStateChangeOutput is the schema-aligned state-change envelope.
type ApprovalStateChangeOutput = SuccessEnvelope[ApprovalStateChangeData]

// BatchOpResultData is one line of batch output — the per-op result envelope.
type BatchOpResultData struct {
	Line           int           `json:"line"`
	Status         CommandStatus `json:"status"`
	Command        string        `json:"command"`
	IdempotencyKey string        `json:"idempotency_key,omitempty"`
	CommandID      string        `json:"command_id,omitempty"`
	RequestID      string        `json:"request_id,omitempty"`
	HTTPStatus     int           `json:"http_status,omitempty"`
	FromCache      bool          `json:"from_cache,omitempty"`
	Data           any           `json:"data,omitempty"`
	Error          string        `json:"error,omitempty"`
	Code           string        `json:"code,omitempty"`
}

// BatchOpResultOutput is the schema-aligned batch op result envelope.
type BatchOpResultOutput = SuccessEnvelope[BatchOpResultData]

// Headers implements TabularData for DoctorData (renders the feature map).
func (d DoctorData) Headers() []string {
	return []string{"COMMAND", "STATUS", "REASON"}
}

// Rows implements TabularData for DoctorData.
func (d DoctorData) Rows() [][]string {
	rows := make([][]string, 0, len(d.Features))
	for _, f := range d.Features {
		rows = append(rows, []string{f.Command, f.Status, f.Reason})
	}
	return rows
}

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

// Headers implements TabularData for SocialMetadataData.
func (d SocialMetadataData) Headers() []string {
	return []string{"URN", "POST", "COMMENTS", "LIKES", "REACTIONS", "STATE"}
}

// Rows implements TabularData for SocialMetadataData.
func (d SocialMetadataData) Rows() [][]string {
	rows := make([][]string, 0, len(d.Items))
	for _, item := range d.Items {
		urn := item.PostURN
		if len(urn) > 15 {
			urn = "..." + urn[len(urn)-12:]
		}
		rows = append(rows, []string{
			urn,
			item.PostURN,
			strconv.Itoa(item.CommentCount),
			strconv.Itoa(item.LikeCount),
			strconv.Itoa(item.ReactionCount),
			item.CommentsState,
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
