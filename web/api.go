package main

import "time"

// The sync API between a local deployment and a remote. Local sends its own
// file ids (project__type); the remote adds the key's username in front.
// Every call carries "Authorization: Bearer <API key>".
//
//	GET /api/types            -> typesResponse
//	PUT /api/files/{id}       pushRequest -> pushResponse
//	GET /api/priorities       -> map[id]priorityEntry, the key owner's files
//
// Wrong keys from one address wait on the login penalty schedule (429 with
// Retry-After, then 403 once blocked); valid calls are limited per account
// (429 with Retry-After). A push larger than the limit gets 413; a new
// project over the account's cap gets 409.

// maxReply caps what local reads of a remote's reply.
const maxReply = 1 << 20

type typesResponse struct {
	All    bool       `json:"all"`   // accepts every project type
	Types  []string   `json:"types"` // otherwise, only these
	Limits *apiLimits `json:"limits,omitempty"`
}

// apiLimits are the remote's limits, so local can stay under them. A remote
// from before they existed sends none.
type apiLimits struct {
	RatePerMinute int `json:"rate_per_minute"` // valid calls per account; 0 = no limit
	MaxFileBytes  int `json:"max_file_bytes"`
	MaxProjects   int `json:"max_projects"` // per account; 0 = no limit
}

// pushRequest always carries the content. Priority and Archived are sent
// only when they changed on local since the last push, so a content push
// never undoes a priority set on the remote or an archive done there.
type pushRequest struct {
	Content  string  `json:"content"`
	Priority *string `json:"priority,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
}

type priorityEntry struct {
	Priority    string    `json:"priority"`
	PrioritySet time.Time `json:"priority_set"` // the remote's clock
}

type pushResponse = priorityEntry
