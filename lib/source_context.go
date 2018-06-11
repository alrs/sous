package sous

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samsalisbury/semv"
)

type (
	// SourceContext contains contextual information about the source code being
	// built.
	SourceContext struct {
		RootDir, OffsetDir, Branch, Revision string
		Files, ModifiedFiles, NewFiles       []string
		Tags                                 []Tag
		NearestTag                           Tag
		NearestTagName, NearestTagRevision   string
		PrimaryRemoteURL                     string
		RemoteURL                            string
		RemoteURLs                           []string
		DirtyWorkingTree                     bool
		RevisionUnpushed                     bool
		DevBuild                             bool
	}
	// Tag represents a revision control commit tag.
	Tag struct {
		Name, Revision string
	}
)

// ZeroVersion is a "zero" version.
var ZeroVersion = semv.MustParse("0.0.0-unversioned")

// NormalizedOffset returns a relative path from root that is based on workdir.
// Notably, it handles the case where the workdir is in the same physical path
// as root, but via symlinks
func NormalizedOffset(root, workdir string) (string, error) {
	parts := strings.Split(workdir, string(os.PathSeparator))
	for n := range parts {
		prefix := "/" + filepath.Join(parts[0:n+1]...)
		prefix, err := filepath.EvalSymlinks(prefix)
		if err != nil {
			break // this isn't working
		}
		if strings.HasPrefix(prefix, root) {
			mid := prefix[len(root):]
			rest := parts[n+1:]
			workdir = filepath.Join(append([]string{root, mid}, rest...)...)
			break
		}
	}

	relDir, err := filepath.Rel(root, workdir)
	if err != nil {
		return "", err
	}
	workdir = filepath.Join(root, relDir)
	relDir, err = filepath.Rel(root, workdir)
	if err != nil {
		return "", err
	}
	if relDir == "." {
		relDir = ""
	}
	return relDir, nil
}

// Version returns the SourceID.
func (sc *SourceContext) Version() SourceID {
	v := nearestVersion(append([]Tag{sc.NearestTag}, sc.Tags...))

	//v.Meta = sc.Revision //991
	v.DefaultFormat = semv.Complete //XXX issue with semv?

	sv := SourceID{
		Location: SourceLocation{
			Repo: sc.RemoteURL,
			Dir:  sc.OffsetDir,
		},
		Version: v,
	}
	return sv
}

// SourceLocation returns the source location in this context.
func (sc *SourceContext) SourceLocation() SourceLocation {
	return SourceLocation{
		Repo: sc.PrimaryRemoteURL,
		Dir:  sc.OffsetDir,
	}
}

// AbsDir returns the absolute path of this source code.
func (sc *SourceContext) AbsDir() string {
	return filepath.Join(sc.RootDir, sc.OffsetDir)
}

// TagVersion returns a semver string if the most recent tag conforms to a
// semver format. Otherwise it returns an empty string
func (sc *SourceContext) TagVersion() string {
	sid := sc.Version()
	v := sid.Version
	if v.Equals(ZeroVersion) { // works because the build-meta field isn't considered
		return ""
	}
	return v.Format(semv.MajorMinorPatch)
}

var versionStrip = regexp.MustCompile(`^\D*`)

func parseSemverTagWithOptionalPrefix(tagName string) (prefix string, v semv.Version, err error) {
	prefix = versionStrip.FindString(tagName)
	versionString := strings.TrimPrefix(tagName, prefix)
	v, err = semv.Parse(versionString)
	if err != nil {
		var addendum string
		if prefix != "" {
			addendum = fmt.Sprintf(" (ignoring the prefix %q)", prefix)
		}
		return prefix, semv.Version{}, fmt.Errorf("parsing semver version tag %q%s: %s", versionString, addendum, err)
	}
	return prefix, v, err
}

func nearestVersion(tags []Tag) semv.Version {
	for _, t := range tags {
		_, v, err := parseSemverTagWithOptionalPrefix(t.Name)
		if err == nil {
			return v
		}
	}
	return ZeroVersion
}
