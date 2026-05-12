package migrations

import (
	"fmt"
	"io/fs"
)

type SourceDescriptor struct {
	Name              string
	Key               string
	Label             string
	Dialect           string
	Root              fs.FS
	ValidationTargets []string
}

func SourceDescriptorForDialect(dialect string) (SourceDescriptor, error) {
	normalized, err := normalizeDialect(dialect)
	if err != nil {
		return SourceDescriptor{}, err
	}

	filesystems, err := Filesystems()
	if err != nil {
		return SourceDescriptor{}, err
	}
	for _, fsys := range filesystems {
		if fsys.Dialect != normalized {
			continue
		}
		return SourceDescriptor{
			Name:              SourceLabel,
			Key:               SourceLabel,
			Label:             SourceLabel,
			Dialect:           normalized,
			Root:              fsys.FS,
			ValidationTargets: []string{normalized},
		}, nil
	}

	return SourceDescriptor{}, fmt.Errorf("migrations: filesystem for dialect %q not found", normalized)
}
