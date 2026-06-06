package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString
#Identifier:     =~"^[A-Za-z_][A-Za-z0-9_]*$"

#Metadata: {
	name: #NonEmptyString
}

#PostgreSQLCluster: {
	apiVersion: "postgresql.guardianintelligence.org/v1alpha1"
	kind:       "PostgreSQLCluster"
	metadata:   #Metadata
	spec: {
		runtimeArtifact:              #RepoPath
		runtimeRoot:                  #AbsolutePath
		dataDir:                      #AbsolutePath
		configDir:                    #AbsolutePath
		logDir:                       #AbsolutePath
		socketDir:                    #AbsolutePath
		reportPath:                   #AbsolutePath
		listenAddress:                #Host
		port:                         int & >=1 & <=65535
		maxConnections:               int & >0
		superuserReservedConnections: int & >=0 & <maxConnections
		databases?: [...{
			name:  #Identifier
			owner: #Identifier
		}]
		roles?: [...{
			name:   #Identifier
			login?: bool
			memberOf?: [...#Identifier]
		}]
		peerMappings?: [...{
			systemUser:   #Identifier
			postgresUser: #Identifier
		}]
	}
}
