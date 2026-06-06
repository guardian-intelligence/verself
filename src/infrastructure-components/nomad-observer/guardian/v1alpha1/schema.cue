package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#HTTPURL:        =~"^https?://[^\\s]+$"
#UnixSocketURL:  =~"^unix:///[^\\s]+$"

#Metadata: {
	name: #NonEmptyString
}

#NomadObserver: {
	apiVersion: "nomadobserver.guardianintelligence.org/v1alpha1"
	kind:       "NomadObserver"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		dataDir:         #AbsolutePath
		graphPath:       #AbsolutePath
		reportPath:      #AbsolutePath

		user:                #NonEmptyString
		group:               #NonEmptyString
		supplementaryGroups: [...#NonEmptyString]

		nomad: {
			address:   #HTTPURL
			namespace: #NonEmptyString
		}

		otel: {
			exporterEndpoint: #HTTPURL
		}

		capture: {
			workers:         int & >=1 & <=32
			queueSize:       int & >=1 & <=4096
			stderrTailBytes: int & >=0 & <=1048576
			stdoutTailBytes: int & >=0 & <=1048576
		}

		fleet: {
			snapshotIntervalSeconds: int & >=5 & <=3600
		}

		clickhouse: {
			address:    #NonEmptyString
			user:       #NonEmptyString
			caCertPath: #AbsolutePath
		}

		spiffe: {
			endpointSocket: #UnixSocketURL
		}
	}
}
