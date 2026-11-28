package main

import (
	"net/http"
	"strings"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of invites and governance (internal/api/governance*.go, T034, T035). Add a probe for
// every route the feature registers, with the routes only maintainers may use in maintainerOnly; see registerProbes.
// The invite routes themselves (list, create, revoke) are internal/auth's, in probes_auth_test.go.
func init() {
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			// Every member: whether they may invite now.
			"GET " + api.InviteStatusPath: {
				{Path: api.InviteStatusPath, Status: http.StatusOK},
			},
			// Maintainers only.
			"POST " + api.InvitePermissionPath: {
				{Method: http.MethodPost, Path: memberPath(api.InvitePermissionPath, idscan.ResetMemberUUID),
					Header: csrfHeader, Body: `{"canInvite":true}`, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.InvitePermissionPath, idscan.ResetMemberUUID),
					Header: csrfHeader, Body: `{}`, Status: http.StatusBadRequest},
				{Method: http.MethodPost, Path: memberPath(api.InvitePermissionPath, idscan.LeftMemberUUID),
					Header: csrfHeader, Body: `{"canInvite":false}`, Status: http.StatusNotFound},
				{Method: http.MethodPost, Path: memberPath(api.InvitePermissionPath, "x"),
					Header: csrfHeader, Body: `{"canInvite":false}`, Status: http.StatusNotFound},
			},
			"POST " + api.InviteEarlyPath: {
				{Method: http.MethodPost, Path: memberPath(api.InviteEarlyPath, idscan.ResetMemberUUID),
					Header: csrfHeader, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.InviteEarlyPath, idscan.MaintainerUUID),
					Header: csrfHeader, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.InviteEarlyPath, "0190a1b2-0000-7000-8000-00000000ffff"),
					Header: csrfHeader, Status: http.StatusNotFound},
			},
		},
		maintainerOnly: []string{"POST " + api.InvitePermissionPath, "POST " + api.InviteEarlyPath},
	})
}

// memberPath fills the {uuid} of a member route.
func memberPath(pattern, uuid string) string {
	return strings.Replace(pattern, "{uuid}", uuid, 1)
}
