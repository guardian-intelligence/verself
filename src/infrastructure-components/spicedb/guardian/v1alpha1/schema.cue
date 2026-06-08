package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString
#HTTPURL:        =~"^https?://[^\\s]+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#SpiceDBCluster: {
	apiVersion: "spicedb.guardianintelligence.org/v1alpha1"
	kind:       "SpiceDBCluster"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		homeDir:         #AbsolutePath
		reportPath:      #AbsolutePath
		user:            #NonEmptyString
		group:           #NonEmptyString
		datastore: {
			engine:           "postgres"
			connURI:          #NonEmptyString
			readPoolMaxOpen:  int & >0
			readPoolMinOpen:  int & >=0 & <=readPoolMaxOpen
			writePoolMaxOpen: int & >0
			writePoolMinOpen: int & >=0 & <=writePoolMaxOpen
		}
		grpc: {
			host: #Host
			presharedKeyRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
		}
		metrics: {
			host: #Host
		}
		openBao: {
			address: #HTTPURL
			caCert:  #AbsolutePath
		}
	}
}
