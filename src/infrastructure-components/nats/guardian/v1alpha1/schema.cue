package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString
#SPIFFEID:       =~"^spiffe://[^\\s]+$"
#NATSSubject:    #NonEmptyString
#MemorySize:     =~"^[0-9]+(B|Kb|Mb|Gb|Tb)$"

#Metadata: {
	name: #NonEmptyString
}

#AuthorizationUser: {
	spiffeID:       #SPIFFEID
	publishAllow:   [...#NATSSubject]
	subscribeAllow: [...#NATSSubject]
}

#NATSCluster: {
	apiVersion: "nats.guardianintelligence.org/v1alpha1"
	kind:       "NATSCluster"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		configPath:      #AbsolutePath
		helperConfigPath: #AbsolutePath
		dataDir:         #AbsolutePath
		jetStreamDir:    #AbsolutePath
		pidPath:         #AbsolutePath
		reportPath:      #AbsolutePath

		user:          #NonEmptyString
		group:         #NonEmptyString
		workloadGroup: #NonEmptyString

		serverName:     #NonEmptyString
		host:           #Host
		clientPort:     int & >=1 & <=65535
		monitoringPort: int & >=1 & <=65535

		jetstream: {
			maxMemStore:  #MemorySize
			maxFileStore: #MemorySize
		}

		spiffe: {
			agentSocket: #AbsolutePath
			certDir:     #AbsolutePath
		}

		authorization: {
			users: [#AuthorizationUser, ...#AuthorizationUser]
		}
	}
}
