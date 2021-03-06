// +build integration,!no-etcd

package integration

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path"
	"time"

	kclient "github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"

	"github.com/openshift/origin/pkg/client"
	newproject "github.com/openshift/origin/pkg/cmd/experimental/project"
	start "github.com/openshift/origin/pkg/cmd/server"
	cmdutil "github.com/openshift/origin/pkg/cmd/util"
	"github.com/openshift/origin/pkg/cmd/util/tokencmd"
)

func init() {
	requireEtcd()
}

func StartTestServer(args ...string) (start.Config, error) {
	deleteAllEtcdKeys()

	startConfig := start.NewDefaultConfig()

	basedir := path.Join(os.TempDir(), "openshift-integration-tests")

	startConfig.VolumeDir = path.Join(basedir, "volume")
	startConfig.EtcdDir = path.Join(basedir, "etcd")
	startConfig.CertDir = path.Join(basedir, "cert")

	masterAddr := httptest.NewUnstartedServer(nil).Listener.Addr().String()
	fmt.Printf("masterAddr: %#v\n", masterAddr)
	startConfig.MasterAddr.Set(masterAddr)
	startConfig.BindAddr.Set(masterAddr)
	startConfig.EtcdAddr.Set(getEtcdURL())

	assetAddr := httptest.NewUnstartedServer(nil).Listener.Addr().String()
	fmt.Printf("assetAddr: %#v\n", assetAddr)
	startConfig.AssetBindAddr.Set(assetAddr)
	startConfig.AssetPublicAddr.Set(assetAddr)

	startConfig.Complete(args)

	var startError error
	go func() {
		err := startConfig.Start(args)
		if err != nil {
			startError = err
			fmt.Printf("ERROR STARTING SERVER! %v", err)
		}
	}()

	// wait for the server to come up: 35 seconds
	if err := cmdutil.WaitForSuccessfulDial(true, "tcp", masterAddr, 100*time.Millisecond, 1*time.Second, 35); err != nil {
		return *startConfig, err
	}

	stopChannel := make(chan struct{})
	util.Until(
		func() {
			if startError != nil {
				close(stopChannel)
				return
			}

			// confirm that we can actually query from the api server
			client, _, err := startConfig.GetOpenshiftClient()
			if err != nil {
				return
			}
			if _, err := client.Policies("master").List(labels.Everything(), labels.Everything()); err == nil {
				close(stopChannel)
			}
		}, 100*time.Millisecond, stopChannel)

	return *startConfig, startError
}

// StartTestMaster starts up a test master and returns back the startConfig so you can get clients and certs
func StartTestMaster() (start.Config, error) {
	return StartTestServer("master")
}

// StartTestNode starts up a test node and returns back the startConfig so you can get clients and certs
func StartTestNode() (start.Config, error) {
	return StartTestServer("node")
}

// StartTestAllInOne starts up a test all-in-one and returns back the startConfig so you can get clients and certs
func StartTestAllInOne() (start.Config, error) {
	return StartTestServer()
}

// CreateNewProject creates a new project using the clusterAdminClient, then gets a token for the adminUser and returns
// back a client for the admin user
func CreateNewProject(clusterAdminClient *client.Client, clientConfig kclient.Config, projectName, adminUser string) (*client.Client, error) {
	qualifiedUser := "anypassword:" + adminUser
	newProjectOptions := &newproject.NewProjectOptions{
		Client:                clusterAdminClient,
		ProjectName:           projectName,
		AdminRole:             "admin",
		MasterPolicyNamespace: "master",
		AdminUser:             qualifiedUser,
	}

	if err := newProjectOptions.Run(); err != nil {
		return nil, err
	}

	token, err := tokencmd.RequestToken(&clientConfig, nil, adminUser, "password")
	if err != nil {
		return nil, err
	}

	adminClientConfig := clientConfig
	adminClientConfig.BearerToken = token
	adminClientConfig.Username = ""
	adminClientConfig.Password = ""
	adminClientConfig.TLSClientConfig.CertFile = ""
	adminClientConfig.TLSClientConfig.KeyFile = ""
	adminClientConfig.TLSClientConfig.CertData = nil
	adminClientConfig.TLSClientConfig.KeyData = nil

	adminClient, err := client.New(&adminClientConfig)
	if err != nil {
		return nil, err
	}

	return adminClient, nil
}
