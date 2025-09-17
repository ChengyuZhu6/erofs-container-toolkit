package converter

import (
	"context"
	"fmt"
	"path"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/images/converter"
	"github.com/containerd/log"
	sociutils "github.com/erofs/erofs-container-toolkit/pkg/soci"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type SociOptions struct {
	spanSize     int64
	minLayerSize int64
	artifactsDb  *sociutils.ArtifactsDb
	buildToolId  string
}

type SociOption func(*SociOptions) error

func WithSociSpanSize(spanSize int64) SociOption {
	return func(o *SociOptions) error {
		o.spanSize = spanSize
		return nil
	}
}

func WithSociMinLayerSize(minLayerSize int64) SociOption {
	return func(o *SociOptions) error {
		o.minLayerSize = minLayerSize
		return nil
	}
}

func WithSociArtifactsDb(db *sociutils.ArtifactsDb) SociOption {
	return func(o *SociOptions) error {
		o.artifactsDb = db
		return nil
	}
}

func WithSociBuildToolIdentifier(id string) SociOption {
	return func(o *SociOptions) error {
		o.buildToolId = id
		return nil
	}
}

// SociConvertFunc returns a converter.ConvertFunc that integrates SOCI conversion
// into the standard containerd converter flow, similar to EROFS
func SociConvertFunc(opts ...SociOption) converter.ConvertFunc {
	return func(ctx context.Context, cs content.Store, desc ocispec.Descriptor) (*ocispec.Descriptor, error) {
		var options SociOptions

		// Set defaults
		options.spanSize = sociutils.DefaultSpanSize
		options.minLayerSize = sociutils.DefaultMinLayerSize
		options.buildToolId = sociutils.DefaultBuildToolIdentifier

		for _, opt := range opts {
			if err := opt(&options); err != nil {
				return nil, err
			}
		}

		// For SOCI, we don't modify individual layers
		// The actual SOCI index creation happens at the image level
		// This function just passes through layers unchanged
		if !images.IsLayerType(desc.MediaType) {
			return nil, nil
		}

		log.G(ctx).Debugf("SociConvertFunc: passing through layer %s", desc.Digest)
		return &desc, nil
	}
}

// SociImageConvertFunc provides a finalize function that creates SOCI indexes
// after the standard converter has processed the image
func SociImageConvertFunc(client interface{}, opts ...SociOption) func(ctx context.Context, cs content.Store, ref string, desc *ocispec.Descriptor) (*images.Image, error) {
	return func(ctx context.Context, cs content.Store, ref string, desc *ocispec.Descriptor) (*images.Image, error) {
		var options SociOptions

		options.spanSize = sociutils.DefaultSpanSize
		options.minLayerSize = sociutils.DefaultMinLayerSize
		options.buildToolId = sociutils.DefaultBuildToolIdentifier

		for _, opt := range opts {
			if err := opt(&options); err != nil {
				return nil, err
			}
		}

		if options.artifactsDb == nil {
			db, err := sociutils.NewDB(path.Join(sociutils.DefaultArtifactsDbPath, sociutils.ArtifactsDbName))
			if err != nil {
				return nil, fmt.Errorf("failed to create SOCI artifacts DB: %w", err)
			}
			options.artifactsDb = db
		}

		containerdClient, ok := client.(*containerd.Client)
		if !ok {
			return nil, fmt.Errorf("client must be of type *client.Client")
		}

		blobStore, err := sociutils.NewContentStore(containerdClient)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCI blob store: %w", err)
		}

		builderOpts := []sociutils.BuilderOption{
			sociutils.WithMinLayerSize(options.minLayerSize),
			sociutils.WithSpanSize(options.spanSize),
			sociutils.WithArtifactsDb(options.artifactsDb),
		}

		builder, err := sociutils.NewIndexBuilder(cs, blobStore, builderOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCI index builder: %w", err)
		}

		img := images.Image{
			Name:   ref,
			Target: *desc,
		}

		batchCtx, done, err := blobStore.BatchOpen(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to open SOCI batch context: %w", err)
		}
		defer done(ctx)

		sociDesc, err := builder.Convert(batchCtx, img,
			sociutils.ConvertWithNoGarbageCollectionLabels(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to convert image to SOCI: %w", err)
		}

		return &images.Image{
			Name:   ref,
			Target: *sociDesc,
		}, nil
	}
}
