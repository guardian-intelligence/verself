package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#HTTPURL:        =~"^https?://[^\\s]+$"
#Identifier:     =~"^[A-Za-z_][A-Za-z0-9_]*$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#ElectricDeployment: {
	apiVersion: "electric.guardianintelligence.org/v1alpha1"
	kind:       "ElectricDeployment"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		image:           #NonEmptyString
		containerd: {
			socketPath: #AbsolutePath
			stateDir:   #AbsolutePath
			rootDir:    #AbsolutePath
		}
		postgres: {
			runtimeRoot: #AbsolutePath
			host:        #NonEmptyString
			port:        int & >=1 & <=65535
		}
		openBao: {
			address: #HTTPURL
			caCert:  #AbsolutePath
		}
		instances: [...{
			name:                #NonEmptyString
			serviceName:         #NonEmptyString
			storageDir:          #AbsolutePath
			database:            #Identifier
			databaseUser:        #Identifier
			connectionLimit:     int & >0
			replicationStreamID: #NonEmptyString
			dbPoolSize:          int & >0
			pgPasswordRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
			apiSecretRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
		}]
	}
}
