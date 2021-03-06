// +build integration,!no-etcd

package integration

import (
	"testing"

	kapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	watchapi "github.com/GoogleCloudPlatform/kubernetes/pkg/watch"

	buildapi "github.com/openshift/origin/pkg/build/api"
	imageapi "github.com/openshift/origin/pkg/image/api"
)

func init() {
	requireEtcd()
}

func TestSimpleImageChangeBuildTrigger(t *testing.T) {
	deleteAllEtcdKeys()
	openshift := NewTestOpenshift(t)
	defer openshift.Close()

	imageRepo := &imageapi.ImageRepository{
		ObjectMeta:            kapi.ObjectMeta{Name: "test-image-trigger-repo"},
		DockerImageRepository: "registry:8080/openshift/test-image-trigger",
		Tags: map[string]string{
			"latest": "ref-1",
		},
	}

	config := imageChangeBuildConfig()

	watch, err := openshift.Client.Builds(testNamespace).Watch(labels.Everything(), labels.Everything(), "0")
	if err != nil {
		t.Fatalf("Couldn't subscribe to Builds %v", err)
	}
	defer watch.Stop()

	created, err := openshift.Client.BuildConfigs(testNamespace).Create(config)
	if err != nil {
		t.Fatalf("Couldn't create BuildConfig: %v", err)
	}
	watch2, err := openshift.Client.BuildConfigs(testNamespace).Watch(labels.Everything(), labels.Everything(), created.ResourceVersion)
	if err != nil {
		t.Fatalf("Couldn't subscribe to BuildConfigs %v", err)
	}
	defer watch2.Stop()

	imageRepo, err = openshift.Client.ImageRepositories(testNamespace).Create(imageRepo)
	if err != nil {
		t.Fatalf("Couldn't create ImageRepository: %v", err)
	}

	// initial build event from the creation of the imagerepo with tag ref-1
	event := <-watch.ResultChan()
	if e, a := watchapi.Added, event.Type; e != a {
		t.Fatalf("expected watch event type %s, got %s", e, a)
	}
	newBuild := event.Object.(*buildapi.Build)
	if newBuild.Parameters.Strategy.DockerStrategy.Image != "registry:8080/openshift/test-image-trigger:ref-1" {
		i, _ := openshift.Client.ImageRepositories(testNamespace).Get(imageRepo.Name)
		bc, _ := openshift.Client.BuildConfigs(testNamespace).Get(config.Name)
		t.Fatalf("Expected build with base image %s, got %s\n, imagerepo is %v\trigger is %s\n", "registry:8080/openshift/test-image-trigger:ref-1", newBuild.Parameters.Strategy.DockerStrategy.Image, i, bc.Triggers[0].ImageChange)
	}
	event = <-watch.ResultChan()
	if e, a := watchapi.Modified, event.Type; e != a {
		t.Fatalf("expected watch event type %s, got %s", e, a)
	}
	newBuild = event.Object.(*buildapi.Build)
	if newBuild.Parameters.Output.DockerImageReference != "registry:8080/openshift/test-image-trigger:outputtag" {
		t.Fatalf("Expected build with output image %s, got %s", "registry:8080/openshift/test-image-trigger:outputtag", newBuild.Parameters.Output.DockerImageReference)
	}
	if newBuild.Labels["testlabel"] != "testvalue" {
		t.Fatalf("Expected build with label %s=%s from build config got %s=%s", "testlabel", "testvalue", "testlabel", newBuild.Labels["testlabel"])
	}
	event = <-watch2.ResultChan()
	updatedConfig, err := openshift.Client.BuildConfigs(testNamespace).Get(config.Name)
	if err != nil {
		t.Fatalf("Couldn't get BuildConfig: %v", err)
	}
	if updatedConfig.Triggers[0].ImageChange.LastTriggeredImageID != "ref-1" {
		t.Errorf("Expected imageID ref-1, got %s", updatedConfig.Triggers[0].ImageChange.LastTriggeredImageID)
	}

	// update the image tag to ref-2 in the imagerepo so we get another build event using that tag.
	imageRepo.Tags["latest"] = "ref-2"
	if _, err = openshift.Client.ImageRepositories(testNamespace).Update(imageRepo); err != nil {
		t.Fatalf("Error updating imageRepo: %v", err)
	}

	event = <-watch.ResultChan()
	if e, a := watchapi.Added, event.Type; e != a {
		t.Fatalf("expected watch event type %s, got %s", e, a)
	}
	newBuild = event.Object.(*buildapi.Build)
	if newBuild.Parameters.Strategy.DockerStrategy.Image != "registry:8080/openshift/test-image-trigger:ref-2" {
		i, _ := openshift.Client.ImageRepositories(testNamespace).Get(imageRepo.Name)
		bc, _ := openshift.Client.BuildConfigs(testNamespace).Get(config.Name)
		t.Fatalf("Expected build with base image %s, got %s\n, imagerepo is %v\trigger is %s\n", "registry:8080/openshift/test-image-trigger:ref-2", newBuild.Parameters.Strategy.DockerStrategy.Image, i, bc.Triggers[0].ImageChange)
	}
	event = <-watch.ResultChan()
	if e, a := watchapi.Modified, event.Type; e != a {
		t.Fatalf("expected watch event type %s, got %s", e, a)
	}
	newBuild = event.Object.(*buildapi.Build)
	if newBuild.Parameters.Output.DockerImageReference != "registry:8080/openshift/test-image-trigger:outputtag" {
		t.Fatalf("Expected build with output image %s, got %s", "registry:8080/openshift/test-image-trigger:outputtag", newBuild.Parameters.Output.DockerImageReference)
	}
	if newBuild.Labels["testlabel"] != "testvalue" {
		t.Fatalf("Expected build with label %s=%s from build config got %s=%s", "testlabel", "testvalue", "testlabel", newBuild.Labels["testlabel"])
	}

	event = <-watch2.ResultChan()
	updatedConfig, err = openshift.Client.BuildConfigs(testNamespace).Get(config.Name)
	if err != nil {
		t.Fatalf("Couldn't get BuildConfig: %v", err)
	}
	if updatedConfig.Triggers[0].ImageChange.LastTriggeredImageID != "ref-2" {
		t.Errorf("Expected imageID ref-2, got %s", updatedConfig.Triggers[0].ImageChange.LastTriggeredImageID)
	}
}

func imageChangeBuildConfig() *buildapi.BuildConfig {
	buildcfg := &buildapi.BuildConfig{
		ObjectMeta: kapi.ObjectMeta{
			Name:   "test-build-cfg",
			Labels: map[string]string{"testlabel": "testvalue"},
		},
		Parameters: buildapi.BuildParameters{
			Source: buildapi.BuildSource{
				Type: "Git",
				Git: &buildapi.GitBuildSource{
					URI: "git://github.com/openshift/ruby-hello-world.git",
				},
				ContextDir: "contextimage",
			},
			Strategy: buildapi.BuildStrategy{
				Type: buildapi.DockerBuildStrategyType,
				DockerStrategy: &buildapi.DockerBuildStrategy{
					Image: "registry:8080/openshift/test-image-trigger",
				},
			},
			Output: buildapi.BuildOutput{
				To: &kapi.ObjectReference{
					Name: "test-image-trigger-repo",
				},
				Tag: "outputtag",
			},
		},
		Triggers: []buildapi.BuildTriggerPolicy{
			{
				Type: buildapi.ImageChangeBuildTriggerType,
				ImageChange: &buildapi.ImageChangeTrigger{
					Image: "registry:8080/openshift/test-image-trigger",
					From: kapi.ObjectReference{
						Name: "test-image-trigger-repo",
					},
					Tag: "latest",
				},
			},
		},
	}
	return buildcfg
}
