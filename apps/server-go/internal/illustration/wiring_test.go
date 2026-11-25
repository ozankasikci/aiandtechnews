package illustration_test

import (
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestBuildPipelineDepsAddsThePhotoStepOnlyWithAFinder(t *testing.T) {
	recent := &fakeRecentPhotos{}
	with := illustration.BuildPipelineDeps(illustration.PipelineConfig{
		Chain: []string{"photo", "source"}, Photos: &fakePhotoFinder{result: realphoto.Result{}}, RecentPhotos: recent,
	})
	if len(with.Chain) != 2 || with.Chain[0].Name != "photo" || with.Chain[0].Photos == nil || with.RecentPhotos == nil {
		t.Fatalf("chain = %+v", with.Chain)
	}
	without := illustration.BuildPipelineDeps(illustration.PipelineConfig{Chain: []string{"photo", "source"}})
	if len(without.Chain) != 1 || without.Chain[0].Name != "source" {
		t.Fatalf("chain without a finder = %+v, want only source", without.Chain)
	}
}
