package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#BucketName:     =~"^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$"
#OpenBaoKV2Path: =~"^[^\\s]+/data/[^\\s]+$"
#Host:           #NonEmptyString
#Identifier:     =~"^[A-Za-z_][A-Za-z0-9_]*$"

#Metadata: {
	name: #NonEmptyString
}

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
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
		backup?: {
			stanza:                       #Identifier
			configPath:                   #AbsolutePath
			spoolDir:                     #AbsolutePath
			logDir:                       #AbsolutePath
			archiveTimeout:               #NonEmptyString
			processMax:                   int & >0
			retentionFull:                int & >0
			destructiveRestoreAllowed?:   bool
			recoveryCredentialOpenBaoPath: #OpenBaoKV2Path
			cipherPassRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
			repository: {
				type:     "s3"
				endpoint: #Host
				region:   #NonEmptyString
				bucket:   #BucketName
				path:     #AbsolutePath
			}
		}
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
