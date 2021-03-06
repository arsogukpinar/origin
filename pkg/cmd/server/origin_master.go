package server

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"time"

	"code.google.com/p/go-uuid/uuid"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/auth/user"
	kclient "github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	clientcmdapi "github.com/GoogleCloudPlatform/kubernetes/pkg/client/clientcmd/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"

	osclient "github.com/openshift/origin/pkg/client"
	"github.com/openshift/origin/pkg/cmd/server/crypto"
	"github.com/openshift/origin/pkg/cmd/server/origin"
)

func (cfg Config) BuildOriginMasterConfig() (*origin.MasterConfig, error) {
	masterAddr, err := cfg.GetMasterAddress()
	if err != nil {
		return nil, err
	}
	kubeAddr, err := cfg.GetKubernetesAddress()
	if err != nil {
		return nil, err
	}
	masterPublicAddr, err := cfg.GetMasterPublicAddress()
	if err != nil {
		return nil, err
	}
	kubePublicAddr, err := cfg.GetKubernetesPublicAddress()
	if err != nil {
		return nil, err
	}
	assetPublicAddr, err := cfg.GetAssetPublicAddress()
	if err != nil {
		return nil, err
	}

	corsAllowedOrigins := []string{}
	corsAllowedOrigins = append(corsAllowedOrigins, cfg.CORSAllowedOrigins...)
	// always include the all-in-one server's web console as an allowed CORS origin
	// always include localhost as an allowed CORS origin
	// always include master public address as an allowed CORS origin
	for _, origin := range []string{assetPublicAddr.Host, masterPublicAddr.Host, "localhost", "127.0.0.1"} {
		// TODO: check if origin is already allowed
		corsAllowedOrigins = append(corsAllowedOrigins, origin)
	}

	etcdHelper, err := cfg.newOpenShiftEtcdHelper()
	if err != nil {
		return nil, fmt.Errorf("Error setting up server storage: %v", err)
	}

	masterCertFile, masterKeyFile, err := cfg.GetMasterCert()
	if err != nil {
		return nil, err
	}
	assetCertFile, assetKeyFile, err := cfg.GetAssetCert()
	if err != nil {
		return nil, err
	}

	clientCAs, err := cfg.GetClientCertCAPool()
	if err != nil {
		return nil, err
	}
	apiClientCAs, err := cfg.GetAPIClientCertCAPool()
	if err != nil {
		return nil, err
	}

	kubeClient, kubeClientConfig, err := cfg.GetKubeClient()
	if err != nil {
		return nil, err
	}
	openshiftClient, openshiftClientConfig, err := cfg.GetOpenshiftClient()
	if err != nil {
		return nil, err
	}
	deployerClientConfig, err := cfg.GetOpenshiftDeployerClientConfig()
	if err != nil {
		return nil, err
	}

	openshiftConfigParameters := origin.MasterConfigParameters{
		MasterBindAddr:       cfg.BindAddr.URL.Host,
		AssetBindAddr:        cfg.GetAssetBindAddress(),
		DNSBindAddr:          cfg.DNSBindAddr.URL.Host,
		MasterAddr:           masterAddr.String(),
		KubernetesAddr:       kubeAddr.String(),
		MasterPublicAddr:     masterPublicAddr.String(),
		KubernetesPublicAddr: kubePublicAddr.String(),
		AssetPublicAddr:      assetPublicAddr.String(),

		CORSAllowedOrigins:                corsAllowedOrigins,
		MasterAuthorizationNamespace:      "master",
		OpenshiftSharedResourcesNamespace: "openshift",
		LogoutURI:                         env("OPENSHIFT_LOGOUT_URI", ""),

		EtcdHelper: etcdHelper,

		MasterCertFile: masterCertFile,
		MasterKeyFile:  masterKeyFile,
		AssetCertFile:  assetCertFile,
		AssetKeyFile:   assetKeyFile,
		ClientCAs:      clientCAs,
		APIClientCAs:   apiClientCAs,

		KubeClient:             kubeClient,
		KubeClientConfig:       *kubeClientConfig,
		OSClient:               openshiftClient,
		OSClientConfig:         *openshiftClientConfig,
		DeployerOSClientConfig: *deployerClientConfig,

		ImageFor: cfg.ImageTemplate.ExpandOrDie,
	}
	openshiftConfig, err := origin.BuildMasterConfig(openshiftConfigParameters)
	if err != nil {
		return nil, err
	}

	return openshiftConfig, nil
}

func (cfg Config) BuildAuthConfig() (*origin.AuthConfig, error) {
	masterAddr, err := cfg.GetMasterAddress()
	if err != nil {
		return nil, err
	}
	masterPublicAddr, err := cfg.GetMasterPublicAddress()
	if err != nil {
		return nil, err
	}
	assetPublicAddr, err := cfg.GetAssetPublicAddress()
	if err != nil {
		return nil, err
	}

	apiServerCAs, err := cfg.GetAPIServerCertCAPool()
	if err != nil {
		return nil, err
	}

	// Build the list of valid redirect_uri prefixes for a login using the openshift-web-console client to redirect to
	// TODO: allow configuring this
	// TODO: remove hard-coding of development UI server
	assetPublicAddresses := []string{assetPublicAddr.String(), "http://localhost:9000", "https://localhost:9000"}

	etcdHelper, err := cfg.newOpenShiftEtcdHelper()
	if err != nil {
		return nil, fmt.Errorf("Error setting up server storage: %v", err)
	}
	// Default to a session authenticator (for browsers), and a basicauth authenticator (for clients responding to WWW-Authenticate challenges)
	defaultAuthRequestHandlers := strings.Join([]string{
		string(origin.AuthRequestHandlerSession),
		string(origin.AuthRequestHandlerBasicAuth),
	}, ",")

	ret := &origin.AuthConfig{
		MasterAddr:           masterAddr.String(),
		MasterPublicAddr:     masterPublicAddr.String(),
		AssetPublicAddresses: assetPublicAddresses,
		MasterRoots:          apiServerCAs,
		EtcdHelper:           etcdHelper,

		// Max token ages
		AuthorizeTokenMaxAgeSeconds: envInt("OPENSHIFT_OAUTH_AUTHORIZE_TOKEN_MAX_AGE_SECONDS", 300, 1),
		AccessTokenMaxAgeSeconds:    envInt("OPENSHIFT_OAUTH_ACCESS_TOKEN_MAX_AGE_SECONDS", 3600, 1),
		// Handlers
		AuthRequestHandlers: origin.ParseAuthRequestHandlerTypes(env("OPENSHIFT_OAUTH_REQUEST_HANDLERS", defaultAuthRequestHandlers)),
		AuthHandler:         origin.AuthHandlerType(env("OPENSHIFT_OAUTH_HANDLER", string(origin.AuthHandlerLogin))),
		GrantHandler:        origin.GrantHandlerType(env("OPENSHIFT_OAUTH_GRANT_HANDLER", string(origin.GrantHandlerAuto))),
		// RequestHeader config
		RequestHeaders:      strings.Split(env("OPENSHIFT_OAUTH_REQUEST_HEADERS", "X-Remote-User"), ","),
		RequestHeaderCAFile: GetOAuthRequestHeaderCAFile(),
		// Session config (default to unknowable secret)
		SessionSecrets:       []string{env("OPENSHIFT_OAUTH_SESSION_SECRET", uuid.NewUUID().String())},
		SessionMaxAgeSeconds: envInt("OPENSHIFT_OAUTH_SESSION_MAX_AGE_SECONDS", 300, 1),
		SessionName:          env("OPENSHIFT_OAUTH_SESSION_NAME", "ssn"),
		// Password config
		PasswordAuth: origin.PasswordAuthType(env("OPENSHIFT_OAUTH_PASSWORD_AUTH", string(origin.PasswordAuthAnyPassword))),
		BasicAuthURL: env("OPENSHIFT_OAUTH_BASIC_AUTH_URL", ""),
		HTPasswdFile: env("OPENSHIFT_OAUTH_HTPASSWD_FILE", ""),
		// Token config
		TokenStore:    origin.TokenStoreType(env("OPENSHIFT_OAUTH_TOKEN_STORE", string(origin.TokenStoreOAuth))),
		TokenFilePath: env("OPENSHIFT_OAUTH_TOKEN_FILE_PATH", ""),
		// Google config
		GoogleClientID:     env("OPENSHIFT_OAUTH_GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: env("OPENSHIFT_OAUTH_GOOGLE_CLIENT_SECRET", ""),
		// GitHub config
		GithubClientID:     env("OPENSHIFT_OAUTH_GITHUB_CLIENT_ID", ""),
		GithubClientSecret: env("OPENSHIFT_OAUTH_GITHUB_CLIENT_SECRET", ""),
	}

	return ret, nil

}

func GetOAuthRequestHeaderCAFile() string {
	return env("OPENSHIFT_OAUTH_REQUEST_HEADER_CA_FILE", "")
}

func (cfg Config) newCA() (*crypto.CA, error) {
	masterAddr, err := cfg.GetMasterAddress()
	if err != nil {
		return nil, err
	}

	// Bootstrap CA
	// TODO: store this (or parts of this) in etcd?
	ca, err := crypto.InitCA(cfg.CertDir, fmt.Sprintf("%s@%d", masterAddr.Host, time.Now().Unix()))
	if err != nil {
		return nil, fmt.Errorf("Unable to configure certificate authority: %v", err)
	}

	return ca, nil
}

// GetAPIClientCertCAPool returns the cert pool used to validate client certificates to the API server
func (cfg Config) GetAPIClientCertCAPool() (*x509.CertPool, error) {
	certs, err := cfg.getAPIClientCertCAs()
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	for _, root := range certs {
		roots.AddCert(root)
	}
	return roots, nil
}

// GetClientCertCAPool returns a cert pool containing all client CAs that could be presented (union of API and OAuth)
func (cfg Config) GetClientCertCAPool() (*x509.CertPool, error) {
	roots := x509.NewCertPool()

	// Add CAs for OAuth
	certs, err := cfg.getOAuthClientCertCAs()
	if err != nil {
		return nil, err
	}
	for _, root := range certs {
		roots.AddCert(root)
	}

	// Add CAs for API
	certs, err = cfg.getAPIClientCertCAs()
	if err != nil {
		return nil, err
	}
	for _, root := range certs {
		roots.AddCert(root)
	}

	return roots, nil
}

// GetAPIServerCertCAPool returns the cert pool containing the roots for the API server cert
func (cfg Config) GetAPIServerCertCAPool() (*x509.CertPool, error) {
	ca, err := cfg.newCA()
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	for _, root := range ca.Config.Roots {
		roots.AddCert(root)
	}
	return roots, nil
}

func (cfg Config) getOAuthClientCertCAs() ([]*x509.Certificate, error) {
	caFile := GetOAuthRequestHeaderCAFile()
	if len(caFile) == 0 {
		return nil, nil
	}
	caPEMBlock, err := ioutil.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	certs, err := crypto.CertsFromPEM(caPEMBlock)
	if err != nil {
		return nil, fmt.Errorf("Error reading %s: %s", caFile, err)
	}
	return certs, nil
}

func (cfg Config) getAPIClientCertCAs() ([]*x509.Certificate, error) {
	ca, err := cfg.newCA()
	if err != nil {
		return nil, err
	}
	return ca.Config.Roots, nil
}

func (cfg Config) GetServerCertHostnames() ([]string, error) {
	masterAddr, err := cfg.GetMasterAddress()
	if err != nil {
		return nil, err
	}
	masterPublicAddr, err := cfg.GetMasterPublicAddress()
	if err != nil {
		return nil, err
	}
	kubePublicAddr, err := cfg.GetKubernetesPublicAddress()
	if err != nil {
		return nil, err
	}
	assetPublicAddr, err := cfg.GetAssetPublicAddress()
	if err != nil {
		return nil, err
	}

	// 172.17.42.1 enables the router to call back out to the master
	// TODO: Remove 172.17.42.1 once we can figure out how to validate the master's cert from inside a pod, or tell pods the real IP for the master
	allHostnames := util.NewStringSet("localhost", "127.0.0.1", "172.17.42.1", masterAddr.Host, masterPublicAddr.Host, kubePublicAddr.Host, assetPublicAddr.Host)
	certHostnames := util.StringSet{}
	for hostname := range allHostnames {
		if host, _, err := net.SplitHostPort(hostname); err == nil {
			// add the hostname without the port
			certHostnames.Insert(host)
		} else {
			// add the originally specified hostname
			certHostnames.Insert(hostname)
		}
	}

	return certHostnames.List(), nil
}

func (cfg Config) GetMasterCert() (certFile string, keyFile string, err error) {
	ca, err := cfg.newCA()
	if err != nil {
		return "", "", err
	}

	certHostnames, err := cfg.GetServerCertHostnames()
	if err != nil {
		return "", "", err
	}

	serverCert, err := ca.MakeServerCert("master", certHostnames)
	if err != nil {
		return "", "", err
	}

	return serverCert.CertFile, serverCert.KeyFile, nil
}

func (cfg Config) GetAssetCert() (certFile string, keyFile string, err error) {
	ca, err := cfg.newCA()
	if err != nil {
		return "", "", err
	}

	certHostnames, err := cfg.GetServerCertHostnames()
	if err != nil {
		return "", "", err
	}

	serverCert, err := ca.MakeServerCert("master", certHostnames)
	if err != nil {
		return "", "", err
	}

	return serverCert.CertFile, serverCert.KeyFile, nil
}

func (cfg Config) newClientConfigTemplate() (*clientcmdapi.Config, error) {
	masterAddr, err := cfg.GetMasterAddress()
	if err != nil {
		return nil, err
	}
	masterPublicAddr, err := cfg.GetMasterPublicAddress()
	if err != nil {
		return nil, err
	}

	return &clientcmdapi.Config{
		Clusters: map[string]clientcmdapi.Cluster{
			"master":        {Server: masterAddr.String()},
			"public-master": {Server: masterPublicAddr.String()},
		},
		Contexts: map[string]clientcmdapi.Context{
			"master":        {Cluster: "master"},
			"public-master": {Cluster: "public-master"},
		},
		CurrentContext: "master",
	}, nil
}

func (cfg Config) GetKubeClient() (*kclient.Client, *kclient.Config, error) {
	var err error
	var kubeClientConfig *kclient.Config

	// if we're starting an all in one, make credentials for a kube client.
	if cfg.StartKube {
		kubeClientConfig, err = cfg.MintSystemClientCert("kube-client")
		if err != nil {
			return nil, nil, err
		}

	} else {
		// Get the kubernetes address we're using
		kubeAddr, err := cfg.GetKubernetesAddress()
		if err != nil {
			return nil, nil, err
		}

		// Try to get the kubeconfig
		kubeCfg, ok, err := cfg.GetExternalKubernetesClientConfig()
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			// No kubeconfig was provided, so just make one that points at the specified host
			// It probably won't work (since it has no auth), but they'll get to see failures logged
			kubeCfg = &kclient.Config{Host: kubeAddr.String()}
		}

		// Ensure the kubernetes address matches the one in the config
		if kubeAddr.String() != kubeCfg.Host {
			return nil, nil, fmt.Errorf("The Kubernetes server (%s) must match the server in the provided kubeconfig (%s)", kubeAddr.String(), kubeCfg.Host)
		}

		kubeClientConfig = kubeCfg
	}

	kubeClient, err := kclient.New(kubeClientConfig)
	if err != nil {
		return nil, nil, err
	}

	return kubeClient, kubeClientConfig, nil
}

func (cfg Config) GetOpenshiftClient() (*osclient.Client, *kclient.Config, error) {
	clientConfig, err := cfg.MintSystemClientCert("openshift-client")
	if err != nil {
		return nil, nil, err
	}

	client, err := osclient.New(clientConfig)
	if err != nil {
		return nil, nil, err
	}

	return client, clientConfig, nil
}

func (cfg Config) GetOpenshiftDeployerClientConfig() (*kclient.Config, error) {
	clientConfig, err := cfg.MintSystemClientCert("openshift-deployer", "system:deployers")
	if err != nil {
		return nil, err
	}

	return clientConfig, nil
}

// known certs:
// openshiftClientUser := &user.DefaultInfo{Name: "system:openshift-client"}
// openshiftDeployerUser := &user.DefaultInfo{Name: "system:openshift-deployer", Groups: []string{"system:deployers"}}
// adminUser := &user.DefaultInfo{Name: "system:admin", Groups: []string{"system:cluster-admins"}}
// kubeClientUser := &user.DefaultInfo{Name: "system:kube-client"}
// // One for each node in cfg.GetNodeList()
func (cfg Config) MintSystemClientCert(username string, groups ...string) (*kclient.Config, error) {
	ca, err := cfg.newCA()
	if err != nil {
		return nil, err
	}
	clientConfigTemplate, err := cfg.newClientConfigTemplate()
	if err != nil {
		return nil, err
	}

	qualifiedUsername := "system:" + username
	user := &user.DefaultInfo{Name: qualifiedUsername, Groups: groups}
	config, err := ca.MakeClientConfig(username, user, *clientConfigTemplate)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func (cfg Config) MintNodeCerts() error {
	for _, node := range cfg.NodeList {
		username := "node-" + node
		if _, err := cfg.MintSystemClientCert(username, "system:nodes"); err != nil {
			return err
		}
	}

	return nil
}
