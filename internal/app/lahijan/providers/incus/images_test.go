// Package incus: images_test.go covers the image surface (list, alias resolve,
// copy, delete). The fake seeds no images by default; tests create one then
// resolve its alias.
package incus_test

import (
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImages_CreateListDelete(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	op, err := p.CopyImage(ctx, incus.CopyImageParams{
		Project: "",
		Source: incus.ImageSource{
			Alias: "ubuntu/24.04",
		},
		Aliases: []incus.ImageAlias{{Name: "ubuntu/24.04"}},
		Public:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "Success", op.Status)

	imgs, err := p.ListImages(ctx, "", true)
	require.NoError(t, err)
	require.NotEmpty(t, imgs)
	assert.Equal(t, "fp-ubuntu/24.04", imgs[0].Fingerprint)
}

func TestImages_ResolveAlias(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.CopyImage(ctx, incus.CopyImageParams{
		Source:  incus.ImageSource{Alias: "ubuntu/24.04"},
		Aliases: []incus.ImageAlias{{Name: "ubuntu/24.04"}},
	})
	require.NoError(t, err)

	fp, err := p.ResolveAlias(ctx, "", "ubuntu/24.04")
	require.NoError(t, err)
	assert.Equal(t, "fp-ubuntu/24.04", fp)
}

func TestImages_GetAndDelete(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.CopyImage(ctx, incus.CopyImageParams{
		Source:  incus.ImageSource{Alias: "debian/12"},
		Aliases: []incus.ImageAlias{{Name: "debian/12"}},
	})
	require.NoError(t, err)

	img, err := p.GetImage(ctx, "", "fp-debian/12")
	require.NoError(t, err)
	assert.Equal(t, "debian/12", img.Aliases[0].Name)

	_, err = p.DeleteImage(ctx, "", "fp-debian/12")
	require.NoError(t, err)

	_, err = p.GetImage(ctx, "", "fp-debian/12")
	require.Error(t, err, "deleted image must not be retrievable")
}

func TestFeaturedImages_Filter(t *testing.T) {
	t.Parallel()
	// Pure data call; no daemon round-trip.
	cfg := []string{"ubuntu/24.04", "debian/12", "", "alpine/3.20", "fedora/40"}
	out := incus.FeaturedImages(cfg)
	assert.Equal(t, []string{"ubuntu/24.04", "debian/12", "alpine/3.20", "fedora/40"}, out,
		"empty entries must be filtered out")
}
