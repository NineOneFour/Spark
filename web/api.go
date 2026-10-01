package main

import "time"

// The sync API between a local deployment and a remote. Local sends its own
// file ids (project__type); the remote adds the key's username in front.
// Every call carries "Authorization: Bearer <API key>".
//
//	GET /api/types            -> typesResponse
//	PUT /api/files/{id}       pushRequest -> pushResponse
//	GET /api/priorities       -> map[id]priorityEntry, the key owner's files

// maxPush caps a pushed snapshot; real ones are a few kilobytes.
const maxPush = 1 << 20

type typesResponse struct {
	All   bool     `json:"all"`   // accepts every project type
	Types []string `json:"types"` // otherwise, only these
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
