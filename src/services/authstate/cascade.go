package authstate

import (
	"slices"
	"sort"
	"strings"
	"time"
)

func revokeAccess(d *AccessData) {
	d.Active = false
	d.PreviousDigest = ""
	d.PreviousUntil = time.Time{}
}

// cascadeAccesses runs inside the same repository write fence as the initiating
// change. Descendants retain their secrets, but cannot retain wider authority
// or a longer lifetime than their parent. Empty intersections revoke access.
func cascadeAccesses(records Records, initial []Access) []Access {
	children := map[string][]string{}
	for id, access := range records.Accesses {
		if parent := access.Snapshot().ParentID; parent != "" {
			children[parent] = append(children[parent], id)
		}
	}
	updated := map[string]Access{}
	queue := make([]string, 0, len(initial))
	for _, access := range initial {
		id := access.Snapshot().ID
		updated[id] = access
		queue = append(queue, id)
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		parent := records.Accesses[id]
		if v, ok := updated[id]; ok {
			parent = v
		}
		p := parent.Snapshot()
		for _, childID := range children[id] {
			child := records.Accesses[childID]
			if v, ok := updated[childID]; ok {
				child = v
			}
			d := child.Snapshot()
			if d.Active {
				before := d
				if !p.Active {
					revokeAccess(&d)
				} else if !p.Policy.Contains(d.Policy) {
					policy, ok := intersectPolicies(d.Policy, p.Policy)
					if !ok {
						revokeAccess(&d)
					} else {
						d.Policy = policy
					}
				}
				d.ExpiresAt = minTime(d.ExpiresAt, p.ExpiresAt)
				if d.Active != before.Active || !d.ExpiresAt.Equal(before.ExpiresAt) || !samePolicy(d.Policy, before.Policy) {
					d.Revision++
					updated[childID] = NewAccess(d)
				}
			}
			queue = append(queue, childID)
		}
	}
	ids := make([]string, 0, len(updated))
	for id := range updated {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Access, 0, len(ids))
	for _, id := range ids {
		out = append(out, updated[id])
	}
	return out
}

func samePolicy(a, b Policy) bool {
	return slices.Equal(a.Scopes, b.Scopes) && slices.Equal(a.Resources, b.Resources)
}

func intersectPolicies(child, parent Policy) (Policy, bool) {
	out := Policy{}
	for _, scope := range child.Scopes {
		if parent.Allows(scope) {
			out.Scopes = append(out.Scopes, scope)
		}
	}
	switch {
	case len(child.Resources) == 0:
		out.Resources = slices.Clone(parent.Resources)
	case len(parent.Resources) == 0:
		out.Resources = slices.Clone(child.Resources)
	default:
		for _, c := range child.Resources {
			for _, p := range parent.Resources {
				if c.Bucket != p.Bucket {
					continue
				}
				switch {
				case strings.HasPrefix(c.Prefix, p.Prefix):
					out.Resources = append(out.Resources, c)
				case strings.HasPrefix(p.Prefix, c.Prefix):
					out.Resources = append(out.Resources, p)
				}
			}
		}
		out.Resources = minimalResources(out.Resources)
		if len(out.Resources) == 0 {
			return Policy{}, false
		}
	}
	return out, out.Valid()
}

func minimalResources(resources []Resource) []Resource {
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Bucket != resources[j].Bucket {
			return resources[i].Bucket < resources[j].Bucket
		}
		return resources[i].Prefix < resources[j].Prefix
	})
	out := resources[:0]
	for _, resource := range resources {
		if len(out) > 0 {
			previous := out[len(out)-1]
			if resource.Bucket == previous.Bucket && strings.HasPrefix(resource.Prefix, previous.Prefix) {
				continue
			}
		}
		out = append(out, resource)
	}
	return out
}
