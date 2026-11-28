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
			// The invite tree, removal, branches and roles (T035): maintainers only. The scan's sessions are fresh, so the
			// routes that need a fresh proof reach their handlers.
			"GET " + api.MemberTreePath: {
				{Path: api.MemberTreePath, Status: http.StatusOK},
			},
			"POST " + api.MemberRemovePath: {
				{Method: http.MethodPost, Path: memberPath(api.MemberRemovePath, idscan.RemovableMemberUUID),
					Header: csrfHeader, Body: `{"recommendations":"hide","reason":"Test reason"}`, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.MemberRemovePath, idscan.ResetMemberUUID),
					Header: csrfHeader, Body: `{"recommendations":"keep"}`, Status: http.StatusBadRequest},
				{Method: http.MethodPost, Path: memberPath(api.MemberRemovePath, idscan.MaintainerUUID),
					Header: csrfHeader, Body: `{"recommendations":"keep","reason":"Test reason"}`, Status: http.StatusConflict},
				{Method: http.MethodPost, Path: memberPath(api.MemberRemovePath, idscan.LeftMemberUUID),
					Header: csrfHeader, Body: `{"recommendations":"keep","reason":"Test reason"}`, Status: http.StatusNotFound},
				{Method: http.MethodPost, Path: memberPath(api.MemberRemovePath, "x"),
					Header: csrfHeader, Body: `{"recommendations":"keep","reason":"Test reason"}`, Status: http.StatusNotFound},
			},
			"POST " + api.BranchPreviewPath: {
				{Method: http.MethodPost, Path: memberPath(api.BranchPreviewPath, idscan.MaintainerUUID),
					Header: csrfHeader, Body: `{"action":"remove"}`, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.BranchPreviewPath, idscan.ResetMemberUUID),
					Header: csrfHeader, Body: `{"action":"x"}`, Status: http.StatusBadRequest},
				{Method: http.MethodPost, Path: memberPath(api.BranchPreviewPath, "0190a1b2-0000-7000-8000-00000000ffff"),
					Header: csrfHeader, Body: `{"action":"stop_invites"}`, Status: http.StatusNotFound},
			},
			// The seeded reset member invited nobody: their branch is themselves.
			"POST " + api.BranchPath: {
				{Method: http.MethodPost, Path: memberPath(api.BranchPath, idscan.ResetMemberUUID), Header: csrfHeader,
					Body: `{"action":"stop_invites","affected":["` + idscan.ResetMemberUUID + `"]}`, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.BranchPath, idscan.ResetMemberUUID), Header: csrfHeader,
					Body:   `{"action":"remove","affected":[],"recommendations":"keep","reason":"Test reason"}`,
					Status: http.StatusConflict},
				{Method: http.MethodPost, Path: memberPath(api.BranchPath, idscan.ResetMemberUUID), Header: csrfHeader,
					Body: `{"action":"stop_invites"}`, Status: http.StatusBadRequest},
			},
			"POST " + api.MemberRolePath: {
				{Method: http.MethodPost, Path: memberPath(api.MemberRolePath, idscan.ResetMemberUUID),
					Header: csrfHeader, Body: `{"role":"member"}`, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.MemberRolePath, idscan.MaintainerUUID),
					Header: csrfHeader, Body: `{"role":"member"}`, Status: http.StatusConflict},
				{Method: http.MethodPost, Path: memberPath(api.MemberRolePath, idscan.LeftMemberUUID),
					Header: csrfHeader, Body: `{"role":"maintainer"}`, Status: http.StatusNotFound},
				{Method: http.MethodPost, Path: memberPath(api.MemberRolePath, idscan.ResetMemberUUID),
					Header: csrfHeader, Body: `{"role":"owner"}`, Status: http.StatusBadRequest},
			},
		},
		maintainerOnly: []string{"POST " + api.InvitePermissionPath, "POST " + api.InviteEarlyPath,
			"GET " + api.MemberTreePath, "POST " + api.MemberRemovePath, "POST " + api.BranchPreviewPath,
			"POST " + api.BranchPath, "POST " + api.MemberRolePath},
	})
}

// memberPath fills the {uuid} of a member route.
func memberPath(pattern, uuid string) string {
	return strings.Replace(pattern, "{uuid}", uuid, 1)
}
