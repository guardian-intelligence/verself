package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString
#HTTPSURL:       =~"^https://[^\\s]+$"
#HTTPURL:        =~"^https?://[^\\s]+$"
#DNSName:        =~"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#StalwartMailServer: {
	apiVersion: "stalwart.guardianintelligence.org/v1alpha1"
	kind:       "StalwartMailServer"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		configPath:      #AbsolutePath
		dataDir:         #AbsolutePath
		reportPath:      #AbsolutePath
		user:            #NonEmptyString
		group:           #NonEmptyString
		server: {
			hostname:     #DNSName
			baseURL:      #HTTPSURL
			httpAddr:     #Host
			httpPort:     int & >=1 & <=65535
			smtpAddr:     #Host
			smtpPort:     int & >=1 & <=65535
			otlpEndpoint: #HTTPURL
		}
		database: {
			host: #AbsolutePath
			name: #NonEmptyString
			user: #NonEmptyString
		}
		openBao: {
			address: #HTTPURL
			caCert:  #AbsolutePath
			adminPasswordRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
		}
	}
}
