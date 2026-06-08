package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#HostPort:       =~"^[^\\s:]+:[0-9]+$"

#Metadata: {
	name: #NonEmptyString
}

#TigerBeetleCluster: {
	apiVersion: "tigerbeetle.guardianintelligence.org/v1alpha1"
	kind:       "TigerBeetleCluster"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		dataFile:        #AbsolutePath
		reportPath:      #AbsolutePath

		user:  #NonEmptyString
		group: #NonEmptyString

		clusterID:    int & >=0
		replica:      int & >=0
		replicaCount: int & >0
		addresses:    [#HostPort, ...#HostPort]

		logLevel:      #NonEmptyString
		statsdAddress: #HostPort
		experimental:  bool | *true
	}
}
