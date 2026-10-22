package supermarket

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/workspace"
)

type nativeOnlyResolver struct {
	t     *testing.T
	calls int
}

var errNativeUnavailable = errors.New("native workspace unavailable")

func (r *nativeOnlyResolver) ResolveWorkspaceTarget(ctx context.Context, _ string, targetID string) (workspace.ResolvedWorkspaceTarget, error) {
	r.calls++
	if targetID != workspace.WorkspaceTargetNative || workspace.WorkspaceTargetFromContext(ctx) != workspace.WorkspaceTargetNative {
		r.t.Fatalf("software management resolved a device: explicit=%q context=%q", targetID, workspace.WorkspaceTargetFromContext(ctx))
	}
	return workspace.ResolvedWorkspaceTarget{}, errNativeUnavailable
}

func TestAppSkillsNeverFollowRemoteWorkspaceSelection(t *testing.T) {
	for _, selected := range []string{"", "remote-computer"} {
		t.Run(selected, func(t *testing.T) {
			resolver := &nativeOnlyResolver{t: t}
			installer := NewInstaller(nil, resolver, nil)
			ctx := workspace.WithWorkspaceTarget(context.Background(), selected)
			if _, err := installer.PublishSkills(ctx, "bot", AppDescriptor{}, ""); !errors.Is(err, errNativeUnavailable) {
				t.Fatalf("publish: %v", err)
			}
			if _, err := installer.RemoveSkills(ctx, "bot", "registry", "app", ""); !errors.Is(err, errNativeUnavailable) {
				t.Fatalf("remove: %v", err)
			}
			if resolver.calls != 2 {
				t.Fatalf("resolver calls = %d", resolver.calls)
			}
		})
	}
}
