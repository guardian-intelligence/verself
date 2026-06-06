package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#SPIFFEID:       =~"^spiffe://[^\\s]+$"
#TrustDomain:    =~"^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$"

#Metadata: {
	name: #NonEmptyString
}

#SPIRECluster: {
	apiVersion: "spire.guardianintelligence.org/v1alpha1"
	kind:       "SPIRECluster"
	metadata:   #Metadata
	spec: {
		runtimeArtifact:          #RepoPath
		identityRegistryArtifact: #RepoPath
		runtimeRoot:              #AbsolutePath
		serverConfigPath:         #AbsolutePath
		agentConfigPath:          #AbsolutePath
		serverDataDir:            #AbsolutePath
		agentDataDir:             #AbsolutePath
		serverSocketPath:         #AbsolutePath
		agentSocketPath:          #AbsolutePath
		joinTokenPath:            #AbsolutePath
		reportPath:               #AbsolutePath
		trustDomain:              #TrustDomain
		agentSpiffeID:            #SPIFFEID
		serverAddress:            #NonEmptyString
		serverPort:               int & >=1 & <=65535
		serverUser:               #NonEmptyString
		serverGroup:              #NonEmptyString
		workloadGroup:            #NonEmptyString
		registrarIntervalSeconds: int & >=1 & <=300
	}
}
