package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#SPIFFEID:       =~"^spiffe://[^\\s]+$"
#Duration:       =~"^[0-9]+(ns|us|µs|ms|s|m|h)$"
#Identifier:     =~"^[A-Za-z_][A-Za-z0-9_]*$"

#Metadata: {
	name: #NonEmptyString
}

#TemporalPlatform: {
	apiVersion: "temporal.guardianintelligence.org/v1alpha1"
	kind:       "TemporalPlatform"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		stateDir:        #AbsolutePath
		authConfigPath:  #AbsolutePath
		reportPath:      #AbsolutePath

		user:          #Identifier
		group:         #Identifier
		workloadGroup: #Identifier

		persistence: {
			user:               #Identifier
			socketDir:          #AbsolutePath
			defaultDatabase:    #Identifier
			visibilityDatabase: #Identifier
			defaultMaxConns:    int & >0
			visibilityMaxConns: int & >0
		}

		authorization: {
			systemAdminIDs?:  [...#SPIFFEID]
			systemWriterIDs?: [...#SPIFFEID]
			systemReaderIDs?: [...#SPIFFEID]
			namespaceRoles?: [...{
				spiffeID:  #SPIFFEID
				namespace: #NonEmptyString
				role:      "admin" | "writer" | "reader" | "worker"
			}]
		}

		bootstrap: {
			namespaces: [...{
				name:      #NonEmptyString
				retention: #Duration
			}]
		}
	}
}
