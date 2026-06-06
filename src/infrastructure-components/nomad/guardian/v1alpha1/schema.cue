package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#HTTPURL:        =~"^https?://[^\\s]+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#NomadCluster: {
	apiVersion: "nomad.guardianintelligence.org/v1alpha1"
	kind:       "NomadCluster"
	metadata:   #Metadata
	spec: {
		address:         #HTTPURL
		datacenter:      #NonEmptyString
		namespace:       #NonEmptyString
		runtimeArtifact: #RepoPath
		installRoot:     #AbsolutePath
		configPath:      #AbsolutePath
		servicePath:     #AbsolutePath
		dataDir:         #AbsolutePath
		nodePool:        #NonEmptyString
		dynamicPorts: {
			min: int & >=1024 & <=65535
			max: int & >=1024 & <=65535 & >min
		}
		openbao?: {
			address:            #HTTPURL
			caFile:             #AbsolutePath
			jwtAuthBackendPath: #NonEmptyString
			defaultIdentity: {
				audience: [#NonEmptyString, ...#NonEmptyString]
				ttl:      #NonEmptyString
			}
		}
	}
}
