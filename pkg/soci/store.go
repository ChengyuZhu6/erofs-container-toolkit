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
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/defaults"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Store is the content store interface used by SOCI. Currently implemented by ContainerdStore.
type Store interface {
	Exists(ctx context.Context, target ocispec.Descriptor) (bool, error)
	Fetch(ctx context.Context, target ocispec.Descriptor) (io.ReadCloser, error)
	Push(ctx context.Context, expected ocispec.Descriptor, reader io.Reader) error
	Label(ctx context.Context, target ocispec.Descriptor, label string, value string) error
	Delete(ctx context.Context, dgst digest.Digest) error
	// BatchOpen starts a series of operations that should not be interrupted by garbage collection.
	// It returns a cleanup function that ends the batch, which should be called after
	// all associated content operations are finished.
	BatchOpen(ctx context.Context) (context.Context, CleanupFunc, error)
}

type CleanupFunc func(context.Context) error

func NopCleanup(context.Context) error { return nil }

func NewContentStore(client *containerd.Client) (Store, error) {
	return NewContainerdStore(client)
}

type ContainerdStore struct {
	cs     content.Store
	client *containerd.Client
}

// assert that ContainerdStore implements Store
var _ Store = (*ContainerdStore)(nil)

func NewContainerdStore(client *containerd.Client) (*ContainerdStore, error) {
	return &ContainerdStore{cs: client.ContentStore(), client: client}, nil
}

// Exists returns true iff the described content exists.
func (s *ContainerdStore) Exists(ctx context.Context, target ocispec.Descriptor) (bool, error) {
	_, err := s.cs.Info(ctx, target.Digest)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return false, nil
	}
	return false, err
}

type sectionReaderAt struct {
	content.ReaderAt
	*io.SectionReader
}

// Fetch fetches the content identified by the descriptor.
func (s *ContainerdStore) Fetch(ctx context.Context, target ocispec.Descriptor) (io.ReadCloser, error) {
	ra, err := s.cs.ReaderAt(ctx, target)
	if err != nil {
		return nil, err
	}
	return sectionReaderAt{ra, io.NewSectionReader(ra, 0, ra.Size())}, nil
}

// Push pushes the content, matching the expected descriptor.
// This should be done within a Batch and followed by Label calls to prevent garbage collection.
func (s *ContainerdStore) Push(ctx context.Context, expected ocispec.Descriptor, reader io.Reader) error {
	exists, err := s.Exists(ctx, expected)
	if err != nil {
		return err
	}
	if exists {
		return nil // consistent with content.Copy behavior
	}

	writer, err := content.OpenWriter(ctx, s.cs, content.WithRef(expected.Digest.String()))
	if err != nil {
		return err
	}
	defer writer.Close()

	buf := make([]byte, defaults.DefaultMaxRecvMsgSize/2)
	written, err := io.CopyBuffer(writer, reader, buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	if expected.Size > 0 && expected.Size != written {
		return fmt.Errorf("unexpected copy size %d, expected %d: %w", written, expected.Size, errdefs.ErrFailedPrecondition)
	}

	if err = writer.Commit(ctx, expected.Size, expected.Digest); err != nil && !IsErrAlreadyExists(err) {
		return err
	}
	return nil
}

// LabelGCRoot labels the target resource to prevent garbage collection of itself.
func LabelGCRoot(ctx context.Context, store Store, target ocispec.Descriptor) error {
	return store.Label(ctx, target, "containerd.io/gc.root", time.Now().Format(time.RFC3339))
}

// LabelGCRefContent labels the target resource to prevent garbage collection of another resource identified by digest
// with an optional ref to allow and disambiguate multiple content labels.
func LabelGCRefContent(ctx context.Context, store Store, target ocispec.Descriptor, ref string, digest string) error {
	if len(ref) > 0 {
		ref = "." + ref
	}
	return store.Label(ctx, target, "containerd.io/gc.ref.content"+ref, digest)
}

// Label creates or updates the named label with the given value.
func (s *ContainerdStore) Label(ctx context.Context, target ocispec.Descriptor, name string, value string) error {
	info := content.Info{
		Digest: target.Digest,
		Labels: map[string]string{name: value},
	}
	_, err := s.cs.Update(ctx, info, "labels."+name)
	return err
}

// Delete removes the described content.
func (s *ContainerdStore) Delete(ctx context.Context, dgst digest.Digest) error {
	return s.cs.Delete(ctx, dgst)
}

// BatchOpen creates a lease, ensuring that no content created within the batch will be garbage collected.
// It returns a cleanup function that ends the lease, which should be called after content is created and labeled.
func (s *ContainerdStore) BatchOpen(ctx context.Context) (context.Context, CleanupFunc, error) {
	ctx, leaseDone, err := s.client.WithLease(ctx)
	if err != nil {
		return ctx, NopCleanup, fmt.Errorf("unable to open batch: %w", err)
	}
	return ctx, leaseDone, nil
}
