// server/store/models.go — shared data models for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Every persistent entity is defined here as a plain Go struct.
// Packages that need a model import store; they never redeclare fields.
//
// Setting key constants are also defined here so any package can reference
// them without importing a separate package.
package store

import "time"

// ─── Setting key constants ────────────────────────────────────────────────────
//
// All keys are defined as typed constants to prevent typos and make
// "find all references" work reliably across the codebase.

const (
	// SettingInviteRequired controls whether new registrations need an invite code.
	// "1" = invite required (default, beta-safe), "0" = open registration.
	SettingInviteRequired = "invite_required"

	// SettingInviteCodeExpiresDays is how many days an unused invite code stays valid.
	SettingInviteCodeExpiresDays = "invite_code_expires_days"

	// SettingProfileBioMaxChars is the maximum character count for the profile bio.
	SettingProfileBioMaxChars = "profile_bio_max_chars"

	// SettingAvatarMaxBytes is the maximum avatar image file size in bytes.
	SettingAvatarMaxBytes = "avatar_max_bytes"

	// SettingCardDescriptionMaxChars is the maximum characters for the readme card description.
	SettingCardDescriptionMaxChars = "card_description_max_chars"

	// SettingFeedPageSize is the number of cards returned per feed page.
	SettingFeedPageSize = "feed_page_size"

	// SettingCommentMaxChars is the maximum character length for a project comment body.
	SettingCommentMaxChars = "comment_max_chars"

	// SettingCommentPageSize is the number of comments returned per page.
	SettingCommentPageSize = "comment_page_size"

	// ─── Black-box / Template parser limits ───────────────────────────────────
	//
	// These settings cap the structural complexity of a single black-box or
	// template device file. They apply globally to every parse call and can
	// be overridden per-user via the user_parser_limits table.

	// SettingParserMaxMethods is the maximum number of exported non-Init methods
	// per device struct. Exceeding this limit is a hard parse error.
	SettingParserMaxMethods = "parser_max_methods"

	// SettingParserMaxInputs is the maximum number of input ports (parameters)
	// per method. Excess inputs are truncated; a soft warning is emitted.
	SettingParserMaxInputs = "parser_max_inputs"

	// SettingParserMaxOutputs is the maximum number of output ports (return
	// values) per method. Excess outputs are truncated; a soft warning is emitted.
	SettingParserMaxOutputs = "parser_max_outputs"

	// SettingParserMaxProps is the maximum number of prop-tagged struct fields
	// per device. Excess props are truncated; a soft warning is emitted.
	SettingParserMaxProps = "parser_max_props"

	// ─── Stage file limits ────────────────────────────────────────────────

	// SettingStageFileMaxPerUser is the global default for the maximum number
	// of saved stage files (IDE scenes) per user. Can be overridden per-group
	// (stage_file_group_limits) or per-user (stage_file_user_limits).
	SettingStageFileMaxPerUser = "stage_file_max_per_user"

	// ─── Project help-file quotas ────────────────────────────────────────
	//
	// Help files are markdown / image / SVG assets the user authors inside
	// the wizard that travel with a published device (`readme.<lang>.md`,
	// per-method `.md`, illustrations under `examples/`, etc.). They live
	// in the `project_help_files` table as SQLite blobs; the two settings
	// below cap how many bytes a project and a user may store in total.
	//
	// Both limits are enforced server-side by handlers under
	// /api/v1/projects/:id/files/help/* on every PUT and rename. They are
	// read fresh on each request via store.GetSettingInt so an admin can
	// adjust them at runtime (today: directly in SQL; later: an admin UI)
	// without restarting the server.
	//
	// Per-user override (e.g. higher quota for a paying customer) is NOT
	// implemented yet. When needed, mirror the user_parser_limits table
	// (see db_parser_limits.go) and add a fallback chain
	// "user override -> global default -> hardcoded fallback".

	// SettingHelpFilesMaxBytesPerProject caps the total size of all help
	// files belonging to a single project. Default seeded value is 5 MB.
	SettingHelpFilesMaxBytesPerProject = "help_files_max_bytes_per_project"

	// SettingHelpFilesMaxBytesPerUser caps the total size of all help files
	// across every project a user owns. Default seeded value is 50 MB.
	SettingHelpFilesMaxBytesPerUser = "help_files_max_bytes_per_user"
)

// ─── User ─────────────────────────────────────────────────────────────────────

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
	// RoleOfficialSpecialist is a trusted specialist employed or contracted by
	// IoTMaker. Their devices and templates can be promoted to the main menu by
	// the admin without going through the regular community review process.
	RoleOfficialSpecialist = "official_specialist"
)

// User represents a registered account.
// This struct covers authentication only — public-facing data lives in UserProfile.
type User struct {
	ID              string    `json:"id"`
	Username        string    `json:"username"`
	Email           string    `json:"email"`
	PasswordHash    string    `json:"-"`
	Role            string    `json:"role"`
	Verified        bool      `json:"verified"`
	PreferredLocale string    `json:"preferredLocale"`
	CountryCode     string    `json:"countryCode"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// PublicUser is a safe subset of User returned to the client after login.
// It never includes the password hash.
type PublicUser struct {
	ID              string `json:"id"`
	Username        string `json:"username"`
	Email           string `json:"email"`
	Role            string `json:"role"`
	PreferredLocale string `json:"preferredLocale"`
	CountryCode     string `json:"countryCode"`
}

func (u *User) Public() PublicUser {
	return PublicUser{
		ID:              u.ID,
		Username:        u.Username,
		Email:           u.Email,
		Role:            u.Role,
		PreferredLocale: u.PreferredLocale,
		CountryCode:     u.CountryCode,
	}
}

// ─── User Profile ─────────────────────────────────────────────────────────────

// UserProfile holds public-facing information for a user account.
//
// The relationship is 1:1 with users, stored in a separate table so that
// authentication queries (login, token validation) never need to load
// profile data. The profile row is created during registration and is
// always present for verified users.
//
// display_name is the public name shown in the marketplace feed. It differs
// from username in two ways: it can contain spaces and unicode characters,
// and it is optional (empty display_name falls back to username in the UI).
type UserProfile struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Bio         string `json:"bio"`
	AvatarURL   string `json:"avatarUrl"`
	GithubURL   string `json:"githubUrl"`
	// GithubUsername is the verified GitHub login obtained via OAuth.
	// Empty string means the user has not connected their GitHub account.
	// Required before a user can submit devices or templates from GitHub.
	GithubUsername string    `json:"githubUsername"`
	WebsiteURL     string    `json:"websiteUrl"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// ProfileUpdate holds the editable fields for a profile PUT request.
// All fields are optional — empty string means "clear the field".
type ProfileUpdate struct {
	DisplayName string `json:"displayName"`
	Bio         string `json:"bio"`
	GithubURL   string `json:"githubUrl"`
	WebsiteURL  string `json:"websiteUrl"`
}

// PublicProfile is the data returned by the public GET /api/v1/users/:username
// endpoint. It combines fields from users and user_profiles but omits private
// data (email, password hash, preferred locale).
type PublicProfile struct {
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Bio         string    `json:"bio"`
	AvatarURL   string    `json:"avatarUrl"`
	GithubURL   string    `json:"githubUrl"`
	WebsiteURL  string    `json:"websiteUrl"`
	MemberSince time.Time `json:"memberSince"`
}

// ─── Invite Code ──────────────────────────────────────────────────────────────

// InviteCode is a single-use registration token created by a verified user.
//
// Lifecycle:
//  1. Created with UsedBy="" and UsedAt=zero value.
//  2. Redeemed during registration: UsedBy is set to the new user's ID,
//     UsedAt is set to the current time.
//  3. Expired codes (ExpiresAt < now) are rejected by the validator even if
//     UsedBy is still empty.
type InviteCode struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	CreatedBy string    `json:"createdBy"`
	UsedBy    string    `json:"usedBy,omitempty"`
	UsedAt    time.Time `json:"usedAt,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// InviteStatus is the computed state of an invite code.
type InviteStatus string

const (
	InviteStatusActive  InviteStatus = "active"
	InviteStatusUsed    InviteStatus = "used"
	InviteStatusExpired InviteStatus = "expired"
)

// Status computes the current state of the invite code.
func (inv *InviteCode) Status() InviteStatus {
	if inv.UsedBy != "" {
		return InviteStatusUsed
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		return InviteStatusExpired
	}
	return InviteStatusActive
}

// ─── Project Settings ─────────────────────────────────────────────────────────

// ProjectSetting is a single server-side configurable value.
type ProjectSetting struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ─── User Parser Limits ───────────────────────────────────────────────────────

// UserParserLimit is a per-user override for a single parser complexity limit.
// When present, it takes precedence over the global project_settings value.
//
// Limit keys correspond to the SettingParser* constants (e.g. SettingParserMaxMethods).
type UserParserLimit struct {
	// UserID is the ID of the user this override applies to.
	UserID string `json:"userId"`

	// LimitKey is the setting key (e.g. "parser_max_methods").
	LimitKey string `json:"limitKey"`

	// Value is the integer limit stored as a string (matches project_settings.value type).
	Value string `json:"value"`

	// Note is an optional admin annotation explaining why the user has a
	// custom limit (e.g. "trusted specialist — ships I2C driver library").
	Note string `json:"note"`

	// UpdatedAt is when this override was last changed.
	UpdatedAt string `json:"updatedAt"`
}

// ─── Locale ───────────────────────────────────────────────────────────────────

// Locale is a supported UI locale shown in the registration form language selector.
type Locale struct {
	Code    string `json:"code"`
	Display string `json:"display"`
}

// ─── Project Category ─────────────────────────────────────────────────────────

// ProjectCategory is a top-level component taxonomy entry.
type ProjectCategory struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	IconFA    string `json:"iconFa,omitempty"`
}

// ─── Project Subcategory ──────────────────────────────────────────────────────

// ProjectSubcategory is scoped to a parent category.
type ProjectSubcategory struct {
	ID         string `json:"id"`
	CategoryID string `json:"categoryId"`
	Name       string `json:"name"`
	SortOrder  int    `json:"sortOrder"`
	IconFA     string `json:"iconFa,omitempty"`
}

// ─── Programming Language ─────────────────────────────────────────────────────

// ProgrammingLanguage is a supported target language for code generation.
type ProgrammingLanguage struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Display   string `json:"display"`
	SortOrder int    `json:"sortOrder"`
}

// ─── Project UI Language ──────────────────────────────────────────────────────

// ProjectUILanguage is the natural language used for project documentation.
type ProjectUILanguage struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Display   string `json:"display"`
	SortOrder int    `json:"sortOrder"`
}

// ─── Project ──────────────────────────────────────────────────────────────────

const (
	ProjectTypeCustomDevice = "custom_device"
)

const (
	ProjectVisibilityPublic  = "public"
	ProjectVisibilityPrivate = "private"
)

// Project represents a user project.
//
// Publishing flags (PublishToFeed, PublishToSearch, ReadyToUse) are only
// meaningful when Visibility == "public". They are always false for private
// projects and are enforced as such by UpdateProject.
//
// These flags are intentionally not set during project creation — a brand-new
// project is never immediately ready for publication. The owner enables them
// later via the project properties modal, once the project has real content
// and documentation.
type Project struct {
	ID                    string    `json:"id"`
	UserID                string    `json:"userId"`
	Name                  string    `json:"name"`
	Type                  string    `json:"type"`
	Visibility            string    `json:"visibility"`
	ProgrammingLanguageID string    `json:"programmingLanguageId"`
	UILanguageID          string    `json:"uiLanguageId"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`

	// Card fields — extracted from readme.md frontmatter on every save.
	// Used by the feed and search endpoints without reading disk files.
	CardTitle       string `json:"cardTitle"`
	CardImage       string `json:"cardImage"`
	CardDescription string `json:"cardDescription"`
	CardKeywords    string `json:"cardKeywords"`

	// Taxonomy — nullable FK to project_categories / project_subcategories.
	CategoryID    string `json:"categoryId,omitempty"`
	SubcategoryID string `json:"subcategoryId,omitempty"`

	// Publishing flags — enabled by the owner in the project properties modal.
	// All three require Visibility == "public" to have any effect.
	//
	//   PublishToFeed   — the project card appears in the community feed tabs.
	//   PublishToSearch — the project appears in marketplace search results.
	//   ReadyToUse      — the owner has committed to quality and documentation;
	//                     displayed as a quality badge in the feed card.
	PublishToFeed   bool `json:"publishToFeed"`
	PublishToSearch bool `json:"publishToSearch"`
	ReadyToUse      bool `json:"readyToUse"`

	// Joined relations — populated by store queries; never written back.
	ProgrammingLanguage *ProgrammingLanguage `json:"programmingLanguage,omitempty"`
	UILanguage          *ProjectUILanguage   `json:"uiLanguage,omitempty"`
	Category            *ProjectCategory     `json:"category,omitempty"`
	Subcategory         *ProjectSubcategory  `json:"subcategory,omitempty"`
}

// ProjectTypeSlug converts a project type constant to the folder name on disk.
func ProjectTypeSlug(projectType string) string {
	switch projectType {
	case ProjectTypeCustomDevice:
		return "customDevice"
	default:
		return projectType
	}
}

// ─── Project Update ───────────────────────────────────────────────────────────

// ProjectUpdate holds the mutable fields the owner can change via the project
// properties modal.  It is passed to store.UpdateProject.
//
// Rules enforced by handleUpdateProject before calling UpdateProject:
//   - Name must be non-empty, ≤ 100 chars, and free of reserved path characters.
//   - Visibility must be "public" or "private".
//   - PublishToFeed, PublishToSearch and ReadyToUse are forced to false when
//     Visibility == "private"; the handler zeros them before calling the store.
//   - Name uniqueness per user is enforced by the DB UNIQUE constraint; the
//     store returns ErrConflict on violation.
type ProjectUpdate struct {
	Name            string
	Visibility      string
	PublishToFeed   bool
	PublishToSearch bool
	ReadyToUse      bool
}

// ─── Project Card ─────────────────────────────────────────────────────────────

// ProjectCard holds the fields extracted from readme.md frontmatter.
type ProjectCard struct {
	CardTitle       string
	CardImage       string
	CardDescription string
	CardKeywords    string
	CategoryID      string
	SubcategoryID   string
}

// ─── Project File ─────────────────────────────────────────────────────────────

const (
	ProjectFileSectionCode = "code"
	ProjectFileSectionImg  = "img"
	ProjectFileSectionDocs = "docs"
)

// ProjectFile describes a single file inside a project directory.
// Not persisted — built on the fly from os.ReadDir.
type ProjectFile struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Section   string `json:"section"`
	Protected bool   `json:"protected"`
}

// ProjectFiles groups files by section.
type ProjectFiles struct {
	Code []*ProjectFile `json:"code"`
	Img  []*ProjectFile `json:"img"`
	Docs []*ProjectFile `json:"docs"`
}

// ─── Project Code Version ─────────────────────────────────────────────────────

// ProjectCodeVersion is one versioned snapshot of the Go source code.
type ProjectCodeVersion struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	UserID    string `json:"userId"`
	Version   int    `json:"version"`
	Filename  string `json:"filename"`
	Source    string `json:"source"`
	// LastParseOk records whether the wizard's /parse endpoint
	// returned a successful BlackBoxDef for this exact source at
	// save time. Used by the IDE on project open to decide whether
	// to silently re-parse and populate the Preview tab.
	LastParseOk bool      `json:"lastParseOk"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ProjectCodeVersionMeta is the lightweight version record for list endpoints.
type ProjectCodeVersionMeta struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Version   int       `json:"version"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"createdAt"`
}

// ─── OTP ─────────────────────────────────────────────────────────────────────

const (
	OTPPurposeVerifyEmail    = "verify_email"
	OTPPurposeLoginTwoFactor = "login_2fa"
	OTPPurposeResetPassword  = "reset_password"
	// OTPPurposeRoleChange is used by the control panel when an admin
	// promotes or demotes a user's role. Requires OTP confirmation.
	OTPPurposeRoleChange = "role_change"

	// OTPPurposeMenuChange is used by the control panel when an admin
	// creates, edits, or deletes a branded menu section. Requires OTP
	// confirmation to prevent accidental changes to the IDE main menu.
	OTPPurposeMenuChange = "menu_change"

	// OTPPurposeTranslationsEdit is used by the control panel when an admin
	// saves a full translation bundle for one locale. Each bundle save
	// consumes one OTP so a stolen control token cannot silently rewrite
	// the product UI in bulk.
	OTPPurposeTranslationsEdit = "translations_edit"
)

// OTPCode is a one-time code tied to a user and a specific purpose.
type OTPCode struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Code      string    `json:"-"`
	Purpose   string    `json:"purpose"`
	ExpiresAt time.Time `json:"expiresAt"`
	Used      bool      `json:"used"`
}

// ─── Project Comment ──────────────────────────────────────────────────────────

// Comment represents a user comment on a project.
//
// Optional quality sub-ratings (DocRating, CodeRating) let reviewers score
// documentation and code quality independently from the overall 1-5 star rating.
// Both fields default to 0 (= not rated). The handler accepts 0 as "no rating"
// and validates 1-5 when a value is present.
//
// Comments are append-only — there is no edit endpoint. This preserves the
// integrity of the review record and discourages post-hoc rewriting.
// A user can delete their own comment; admins can delete any comment.
type Comment struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	UserID    string `json:"userId"`
	Body      string `json:"body"`

	// Optional sub-ratings. 0 = not provided; 1–5 = quality score.
	DocRating  int `json:"docRating"`  // documentation quality
	CodeRating int `json:"codeRating"` // code quality

	CreatedAt time.Time `json:"createdAt"`

	// Joined fields — populated by ListComments, never stored.
	AuthorUsername    string `json:"authorUsername"`
	AuthorDisplayName string `json:"authorDisplayName"`
	AuthorAvatarURL   string `json:"authorAvatarUrl"`
}

// ─── Project Report ───────────────────────────────────────────────────────────

// Report report categories — a fixed vocabulary enforced by the handler.
// Using constants prevents typos and makes "find all references" reliable.
const (
	// ReportReasonOffensive — content that is harmful, abusive, or
	// violates community norms.
	ReportReasonOffensive = "offensive"

	// ReportReasonOffTopic — the project content (files, description) is
	// unrelated to the IoTMaker community purpose (e.g. random files, memes).
	ReportReasonOffTopic = "off_topic"

	// ReportReasonSpam — the project is clearly spam, advertisement, or
	// duplicate content with no technical value.
	ReportReasonSpam = "spam"

	// ReportReasonMisleading — the project description or code is intentionally
	// misleading (e.g. claims to do something it does not).
	ReportReasonMisleading = "misleading"
)

// ReportReasons is the exhaustive list of allowed report reason codes.
// Used by the handler for input validation.
var ReportReasons = []string{
	ReportReasonOffensive,
	ReportReasonOffTopic,
	ReportReasonSpam,
	ReportReasonMisleading,
}

// Report status constants — used by the moderation workflow.
const (
	ReportStatusPending   = "pending"   // awaiting moderator review
	ReportStatusReviewed  = "reviewed"  // moderator has looked at it; action taken if needed
	ReportStatusDismissed = "dismissed" // moderator determined no action needed
)

// Report represents a user report filed against a project.
//
// One user can file at most one report per project (UNIQUE constraint on
// user_id + project_id). Attempting to report the same project again returns
// ErrConflict — the handler surfaces this as a 409 with a clear message.
//
// The `details` field is free text limited to 500 characters. It is optional
// but encouraged so moderators understand the context.
//
// The `status` field is managed by moderators through a future admin panel.
// All new reports start as "pending".
type Report struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	UserID    string    `json:"userId"`
	Reason    string    `json:"reason"`  // one of ReportReasons
	Details   string    `json:"details"` // optional free-text explanation
	Status    string    `json:"status"`  // pending | reviewed | dismissed
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ─── Template Package ─────────────────────────────────────────────────────────

// Template package status constants track where the package is in its lifecycle.
//
// Status transitions for the parent template_packages row:
//
//	no_version → pending  (specialist uploads the first ZIP version)
//	pending    → ready    (worker parsed the ZIP successfully)
//	pending    → error    (worker encountered a fatal parse error)
//	ready      → pending  (specialist uploads a new ZIP version — replaces the current)
//	error      → pending  (specialist re-uploads to fix parse errors)
const (
	// TemplatePkgStatusNoVersion is the initial status of a newly created
	// template package before the specialist uploads any ZIP version.
	// The package is not usable in the IDE until at least one version is uploaded.
	TemplatePkgStatusNoVersion = "no_version"

	// TemplatePkgStatusPending means a ZIP version was uploaded and queued for
	// the worker. The package is not yet usable in the IDE.
	TemplatePkgStatusPending = "pending"

	// TemplatePkgStatusReady means the worker successfully parsed the latest ZIP.
	// The package is available to makers in the IDE template picker.
	TemplatePkgStatusReady = "ready"

	// TemplatePkgStatusError means the worker encountered a fatal parse error on
	// the latest ZIP. The specialist must upload a corrected version.
	TemplatePkgStatusError = "error"
)

// Template package visibility constants.
// Visibility starts as private and can only be changed by the owner.
const (
	// TemplatePkgVisibilityPrivate means only the uploading specialist can see
	// and use this template in the IDE.
	TemplatePkgVisibilityPrivate = "private"

	// TemplatePkgVisibilityPublic means any authenticated maker on the platform
	// can use this template. The specialist opts into this explicitly.
	TemplatePkgVisibilityPublic = "public"
)

// TemplatePackage is a full Go project template published by a specialist on GitHub.
//
// The specialist publishes a Go project on GitHub, creates a release tag, and
// submits the release URL here. The server downloads the ZIP, parses IDS structs
// and the full project, and stores the result in template_package_versions.
//
// Placeholder format in template source: {{.StructName.FieldName}}
// where FieldName is a prop-tagged field in an IDS struct.
// No template.json needed — the parser infers mappings from props.
//
// Lifecycle:
//  1. Specialist creates a record (name, description) — status=no_version.
//  2. Submits a GitHub release URL → worker downloads ZIP, parses, saves def_json.
//  3. Status becomes ready (or error). Specialist can re-submit a new tag.
//  4. Maker selects template, configures it → code generation (future).
//
// Publishing flags mirror the Project model. Only active when Visibility==public.
type TemplatePackage struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Visibility controls who can see and use the template.
	Visibility string `json:"visibility"` // TemplatePkgVisibility* constants

	// Status tracks the parse lifecycle.
	// no_version → pending → ready | error
	Status string `json:"status"` // TemplatePkgStatus* constants

	// LatestVersion is the highest version number submitted so far.
	LatestVersion int `json:"latestVersion"`

	// GitHub fields — the source of the template code.
	GithubURL   string `json:"githubUrl"`
	GithubOwner string `json:"githubOwner"`
	GithubRepo  string `json:"githubRepo"`
	GithubTag   string `json:"githubTag"`

	// Tags is a comma-separated list of searchable tags (e.g. "ecommerce,postgresql").
	Tags string `json:"tags,omitempty"`

	// DisplayNameHuman is extracted from the first # heading in readme.md.
	// Falls back to "owner/repo" when no readme.md is present.
	DisplayNameHuman string `json:"displayNameHuman,omitempty"`

	// CategoryID and SubcategoryID place the template in the IDE menu taxonomy.
	CategoryID    string `json:"categoryId,omitempty"`
	SubcategoryID string `json:"subcategoryId,omitempty"`

	// Blocked — when 1, the template is hidden and endpoints return 403.
	Blocked int `json:"blocked,omitempty"`

	ParseErrors []string `json:"parseErrors,omitempty"`

	// Publishing flags — only active when Visibility == "public" and status == "ready".
	PublishToFeed   bool `json:"publishToFeed"`
	PublishToSearch bool `json:"publishToSearch"`
	ReadyToUse      bool `json:"readyToUse"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TemplatePackageMeta is the lightweight record used by list endpoints.
type TemplatePackageMeta struct {
	ID            string    `json:"id"`
	UserID        string    `json:"userId"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Visibility    string    `json:"visibility"`
	Status        string    `json:"status"`
	LatestVersion int       `json:"latestVersion"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// TemplatePublishingUpdate holds the publishing flags that can be changed
// via PUT /api/v1/templates/:id/publishing. The handler enforces that all
// three flags must be false when the template visibility is "private".
type TemplatePublishingUpdate struct {
	PublishToFeed   bool
	PublishToSearch bool
	ReadyToUse      bool
}

// TemplatePackageVersion is one versioned ZIP upload attached to a template
// package. It mirrors the project_code_versions pattern.
//
// The version number is assigned by the server (MAX(version)+1 per template),
// never by the specialist. Only the highest version is active — the parent
// template_packages row always reflects the state of the latest version.
//
// The parsed definition (devices + output manifest) is stored as JSON in
// def_json so the IDE can load it without re-parsing the ZIP. Parse errors
// from the worker are stored in parse_errors.
type TemplatePackageVersion struct {
	ID          string    `json:"id"`
	PkgID       string    `json:"pkgId"`     // FK → template_packages.id
	UserID      string    `json:"userId"`    // uploader
	Version     int       `json:"version"`   // auto-increment per pkg
	GithubURL   string    `json:"githubUrl"` // GitHub release URL submitted by specialist
	GithubTag   string    `json:"githubTag"` // release tag (e.g. "v1.2")
	DefJSON     string    `json:"-"`         // parsed project definition — not sent to API
	ParseErrors []string  `json:"parseErrors,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}
