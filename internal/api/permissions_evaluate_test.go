package api

// White-box unit coverage for evaluateCandidates, the pure permission-evaluation
// core shared by checkPermissionQ and the batched resolver. Every rule is driven
// with hand-built aclCandidate slices, so no database is required; the DB-backed
// permission behaviour is covered by the permissions_*_test.go suites.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// evalCandidate builds an aclCandidate the way the ACL fetch paths do: the
// subject rank is always derived from the subject type.
func evalCandidate(subjectType, subjectID string, actions []string, specificity int) aclCandidate {
	return aclCandidate{
		subjectType: subjectType,
		subjectID:   subjectID,
		actions:     actions,
		specificity: specificity,
		subjectRank: subjectRank(subjectType),
	}
}

func TestEvaluateCandidates(t *testing.T) {
	c := evalCandidate

	// A matching entry that grants the action allows.
	require.True(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"edit"}, -1),
	}, "u1", "editor", nil, "notebook", "view"))

	// A granting specific entry allows even when a less-specific Everyone
	// entry lacks the action: the Everyone exception never blocks a grant.
	require.True(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"edit"}, -1),
		c("org_role", "everyone", []string{"view"}, 1),
	}, "u1", "editor", nil, "notebook", "edit"))

	// An Everyone grant allows even when a more-specific matching entry is
	// restrictive (matches the user but lacks the action).
	require.True(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"view"}, -1),
		c("org_role", "everyone", []string{"edit"}, 1),
	}, "u1", "editor", []string{"g1"}, "notebook", "edit"))

	// An Everyone entry that does not grant the action cannot allow either.
	require.False(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"view"}, -1),
		c("org_role", "everyone", []string{"view"}, 1),
	}, "u1", "editor", nil, "notebook", "run"))

	// A bare Everyone grant allows.
	require.True(t, evaluateCandidates([]aclCandidate{
		c("org_role", "everyone", []string{"view"}, -1),
	}, "u1", "editor", nil, "notebook", "view"))

	// A restrictive more-specific match with no Everyone grant denies.
	require.False(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"view"}, -1),
	}, "u1", "editor", nil, "notebook", "run"))

	// view is implied by use/edit/share/delete/admin...
	for _, action := range []string{"use", "edit", "share", "delete", "admin"} {
		require.Truef(t, evaluateCandidates([]aclCandidate{
			c("user", "u1", []string{action}, -1),
		}, "u1", "editor", nil, "notebook", "view"), "%s should imply view", action)
	}
	// ...but view never implies another action.
	require.False(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"view"}, -1),
	}, "u1", "editor", nil, "notebook", "edit"))

	// A group subject matches only when its id is among the caller's groups.
	require.True(t, evaluateCandidates([]aclCandidate{
		c("group", "g1", []string{"view"}, -1),
	}, "u1", "editor", []string{"g1"}, "notebook", "view"))
	require.False(t, evaluateCandidates([]aclCandidate{
		c("group", "g2", []string{"view"}, -1),
	}, "u1", "editor", []string{"g1"}, "notebook", "view"))

	// Org-role entries other than "everyone" never match.
	require.False(t, evaluateCandidates([]aclCandidate{
		c("org_role", "editor", []string{"view"}, -1),
	}, "u1", "editor", nil, "notebook", "view"))

	// Ancestor-folder edit grants expand onto the descendant actions — notebook
	// run, connector/tool use, dashboard view_with_data — at any ancestor depth.
	for _, tc := range []struct {
		resourceType string
		action       string
	}{
		{"notebook", "run"},
		{"connector", "use"},
		{"tool", "use"},
		{"dashboard", "view_with_data"},
	} {
		require.Truef(t, evaluateCandidates([]aclCandidate{
			c("user", "u1", []string{"edit"}, 0),
		}, "u1", "editor", nil, tc.resourceType, tc.action),
			"parent-folder edit should imply %s on %s", tc.action, tc.resourceType)
		require.Truef(t, evaluateCandidates([]aclCandidate{
			c("user", "u1", []string{"edit"}, 1),
		}, "u1", "editor", nil, tc.resourceType, tc.action),
			"ancestor-folder edit should imply %s on %s", tc.action, tc.resourceType)
	}

	// A granting folder entry allows despite a more-specific restrictive
	// direct entry (grants at any depth allow; only Everyone is special).
	require.True(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"view"}, -1),
		c("user", "u1", []string{"edit"}, 0),
	}, "u1", "editor", nil, "notebook", "run"))

	// A direct (specificity -1) grant keeps its exact action list: edit never
	// implies run there.
	require.False(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"edit"}, -1),
	}, "u1", "editor", nil, "notebook", "run"))

	// Folder manage expands onto share for descendants.
	require.True(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"manage"}, 0),
	}, "u1", "editor", nil, "notebook", "share"))

	// Folders themselves are excluded from the expansion — they have no
	// descendant actions to imply.
	require.False(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"edit"}, 0),
	}, "u1", "editor", nil, "folder", "run"))
	require.False(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"manage"}, 0),
	}, "u1", "editor", nil, "folder", "share"))

	// A candidate that matches no subject is ignored.
	require.False(t, evaluateCandidates([]aclCandidate{
		c("user", "u2", []string{"edit"}, -1),
	}, "u1", "editor", nil, "notebook", "view"))

	// Deny by default: empty and nil candidate slices deny.
	require.False(t, evaluateCandidates(nil, "u1", "editor", nil, "notebook", "view"))
	require.False(t, evaluateCandidates([]aclCandidate{}, "u1", "editor", []string{"g1"}, "notebook", "view"))
}

// TestEvaluateCandidatesOrdering pins the ordering half of the contract: the
// candidates slice is sorted in place most specific first, then user > group >
// org_role within a depth. The boolean verdict itself is order-independent, so
// the ordering is asserted through the mutated slice.
func TestEvaluateCandidatesOrdering(t *testing.T) {
	candidates := []aclCandidate{
		evalCandidate("org_role", "everyone", []string{"view"}, 1),
		evalCandidate("group", "g1", []string{"view"}, 0),
		evalCandidate("user", "u2", []string{"view"}, 0),
		evalCandidate("user", "u1", []string{"view"}, -1),
	}
	require.True(t, evaluateCandidates(candidates, "u1", "editor", []string{"g1"}, "notebook", "view"))

	require.Equal(t, []int{-1, 0, 0, 1}, []int{
		candidates[0].specificity,
		candidates[1].specificity,
		candidates[2].specificity,
		candidates[3].specificity,
	})
	// Same-depth entries rank user before group before org_role.
	require.Equal(t, []string{"user", "group", "org_role"}, []string{
		candidates[1].subjectType,
		candidates[2].subjectType,
		candidates[3].subjectType,
	})
}
