package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString
#FileMode:       =~"^0[0-7]{3}$"
#SPIFFEID:       =~"^spiffe://[^\\s]+$"

#Metadata: {
	name: #NonEmptyString
}

#ClickHouseCluster: {
	apiVersion: "clickhouse.guardianintelligence.org/v1alpha1"
	kind:       "ClickHouseCluster"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		dataDir:         #AbsolutePath
		backupDir:       #AbsolutePath
		backupDiskName:  #NonEmptyString
		logDir:          #AbsolutePath
		host:            #Host
		port:            int & >=1 & <=65535

		configPath: #AbsolutePath
		tlsDir:     #AbsolutePath
		pidPath:    #AbsolutePath

		serverUser:  #NonEmptyString
		serverGroup: #NonEmptyString

		operatorUser:             #NonEmptyString
		operatorGroup:            #NonEmptyString
		operatorDatabaseUser:     #NonEmptyString
		operatorClientConfigPath: #AbsolutePath
		operatorCAPath:           #AbsolutePath

		spiffe: {
			trustDomain:          #NonEmptyString
			servicePrefix:        #SPIFFEID
			agentSocket:          #AbsolutePath
			helperPath:           #AbsolutePath
			serverID:             #SPIFFEID
			operatorID:           #SPIFFEID
			serverDir:            #AbsolutePath
			operatorDir:          #AbsolutePath
			spireWorkloadGroup:   #NonEmptyString
			serverHelperConfig:   #AbsolutePath
			operatorHelperConfig: #AbsolutePath
			bundleReloadState:    #AbsolutePath
		}

		systemd: {
			serverServicePath:         #AbsolutePath
			serverHelperServicePath:   #AbsolutePath
			operatorHelperServicePath: #AbsolutePath
			bundleReloadServicePath:   #AbsolutePath
			bundleReloadPathUnitPath:  #AbsolutePath
		}

		migrations: {
			bootstrapSchemaPath: #RepoPath
			deltaDir:            #RepoPath
			stateDir:            #AbsolutePath
		}

		clientCAProjections?: [...{
			path:          #AbsolutePath
			group:         #NonEmptyString
			mode:          #FileMode
			directoryMode: #FileMode
		}]

		reportPath: #AbsolutePath
	}
}
