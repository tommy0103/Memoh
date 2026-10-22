package skills

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"

	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

func appendDiscoveryRoots(roots []Root, extra ...Root) []Root {
	if len(extra) == 0 {
		return roots
	}
	seen := make(map[string]struct{}, len(roots)+len(extra))
	for _, root := range roots {
		seen[root.Path] = struct{}{}
	}
	for _, root := range extra {
		if root.Path == "" {
			continue
		}
		if _, ok := seen[root.Path]; ok {
			continue
		}
		seen[root.Path] = struct{}{}
		roots = append(roots, root)
	}
	return roots
}

// orderedDiscoveryRoots defines the source precedence consumed by resolve():
// user (/data/skills/<skill>) > user compatibility (/data/skills/user/personal)
// > built-in > legacy > compat > Registry.
func orderedDiscoveryRoots(ctx context.Context, client fileClient, rawCompatRoots []string) []Root {
	compatUserRoots, registryRoots := discoverManagedSkillRoots(ctx, client)
	roots := []Root{{Path: ManagedDirPath, Kind: SourceKindManaged, Managed: true}}
	roots = appendDiscoveryRoots(roots, compatUserRoots...)
	roots = appendDiscoveryRoots(roots, DiscoveryRoots(rawCompatRoots)...)
	roots = appendDiscoveryRoots(roots, registryRoots...)
	return roots
}

// discoverManagedSkillRoots walks the canonical namespace/app/Skill layout.
func discoverManagedSkillRoots(ctx context.Context, client fileClient) (userRoots, registryRoots []Root) {
	if client == nil {
		return nil, nil
	}
	namespaces, err := client.ListDirAll(ctx, ManagedDirPath, false)
	if err != nil {
		return nil, nil
	}
	slices.SortFunc(namespaces, func(a, b *pb.FileEntry) int {
		return strings.Compare(a.GetPath(), b.GetPath())
	})
	userRoots = make([]Root, 0)
	registryRoots = make([]Root, 0)
	for _, namespaceEntry := range namespaces {
		if !namespaceEntry.GetIsDir() {
			continue
		}
		namespaceID := path.Base(namespaceEntry.GetPath())
		if !IsValidName(namespaceID) {
			continue
		}
		namespacePath, err := skillNamespaceDirForID(namespaceID)
		if err != nil {
			continue
		}
		apps, err := client.ListDirAll(ctx, namespacePath, false)
		if err != nil {
			continue
		}
		slices.SortFunc(apps, func(a, b *pb.FileEntry) int {
			return strings.Compare(a.GetPath(), b.GetPath())
		})
		for _, appEntry := range apps {
			if !appEntry.GetIsDir() {
				continue
			}
			appID := path.Base(appEntry.GetPath())
			if !IsValidName(appID) {
				continue
			}
			appPath, err := AppDirForIDs(namespaceID, appID)
			if err != nil {
				continue
			}
			if namespaceID == UserSkillNamespace {
				userRoots = append(userRoots, Root{Path: appPath, Kind: SourceKindManaged, Managed: true})
				continue
			}
			skills, err := client.ListDirAll(ctx, appPath, false)
			if err != nil {
				continue
			}
			slices.SortFunc(skills, func(a, b *pb.FileEntry) int {
				return strings.Compare(a.GetPath(), b.GetPath())
			})
			for _, skillEntry := range skills {
				if !skillEntry.GetIsDir() {
					continue
				}
				skillID := path.Base(skillEntry.GetPath())
				if !IsValidName(skillID) {
					continue
				}
				skillPath, err := SkillDirForIDs(namespaceID, appID, skillID)
				if err != nil {
					continue
				}
				registryRoots = append(registryRoots, Root{
					Path:    skillPath,
					Kind:    SourceKindRegistry,
					Managed: true,
				})
			}
		}
	}
	return userRoots, registryRoots
}

// HoldsNamespacedSkills reports whether skillDir, a flat /data/skills/<name>
// Skill directory, is also a Skill namespace: discovery walks every directory
// below ManagedDirPath as one, so <name>/<app>/<skill>/SKILL.md entries there
// (the user compatibility directory, an installed Registry) are other Skills.
// Renaming or deleting skillDir as one Skill would take them along. A missing
// directory holds nothing.
//
// This guards a layout ambiguity rather than resolving it: remove it once
// namespaced Skills live under a prefix that no Skill name can take.
func HoldsNamespacedSkills(ctx context.Context, client fileClient, skillDir string) (bool, error) {
	skillDir = path.Clean(strings.TrimSpace(skillDir))
	if path.Dir(skillDir) != ManagedDirPath {
		return false, nil
	}
	apps, err := client.ListDirAll(ctx, skillDir, false)
	if errors.Is(err, bridge.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, appEntry := range apps {
		if !appEntry.GetIsDir() {
			continue
		}
		appDir := path.Join(skillDir, path.Base(appEntry.GetPath()))
		skills, err := client.ListDirAll(ctx, appDir, false)
		if err != nil {
			return false, err
		}
		for _, skillEntry := range skills {
			if !skillEntry.GetIsDir() {
				continue
			}
			nestedDir := path.Join(appDir, path.Base(skillEntry.GetPath()))
			if !IsNamespacedSkillPath(path.Join(nestedDir, "SKILL.md")) {
				continue
			}
			files, err := client.ListDirAll(ctx, nestedDir, false)
			if err != nil {
				return false, err
			}
			for _, file := range files {
				if !file.GetIsDir() && path.Base(file.GetPath()) == "SKILL.md" {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
