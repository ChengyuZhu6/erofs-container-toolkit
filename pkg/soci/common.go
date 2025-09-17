/*
   Copyright The Soci Snapshotter Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package soci

import (
	"errors"
	"fmt"

	"github.com/containerd/errdefs"
	"oras.land/oras-go/v2/errdef"
)

// Default constants used across the SOCI package
const (
	// DefaultSpanSize is the default span size for SOCI indexes (4MiB)
	DefaultSpanSize = int64(1 << 22)

	// DefaultMinLayerSize is the default minimum layer size for creating zTOCs (10MiB)
	DefaultMinLayerSize = int64(10 << 20)

	// DefaultBuildToolIdentifier is the default identifier for the build tool
	DefaultBuildToolIdentifier = "AWS SOCI CLI v0.2"

	// DefaultArtifactsDbPath is the default path for the artifacts database
	DefaultArtifactsDbPath = "/var/lib/soci-snapshotter-grpc/"

	// SociIndexArtifactType is the artifactType of index SOCI index
	SociIndexArtifactType = SociIndexArtifactTypeV1

	// SociIndexArtifactTypeV1 is the artifact type of a v1 SOCI index which
	// uses the subject field and the OCI referrers API
	SociIndexArtifactTypeV1 = "application/vnd.amazon.soci.index.v1+json"

	// SociIndexArtifactTypeV2 is the artifact type of a v2 SOCI index which
	// does not contain a subject and instead maintains a reference via an annotation on an image manifest
	SociIndexArtifactTypeV2 = "application/vnd.amazon.soci.index.v2+json"

	// SociLayerMediaType is the mediaType of ztoc
	SociLayerMediaType = "application/octet-stream"
)

var (
	ErrEmptyIndex = errors.New("no ztocs created, all layers either skipped or produced errors")
)

func IsErrAlreadyExists(err error) bool {
	return errors.Is(err, errdefs.ErrAlreadyExists) || // containerd error
		errors.Is(err, errdef.ErrAlreadyExists) // ORAS error
}

func WrapError(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}
