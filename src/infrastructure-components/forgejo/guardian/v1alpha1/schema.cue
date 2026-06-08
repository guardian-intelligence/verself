package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString
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

#ForgejoInstance: {
	apiVersion: "forgejo.guardianintelligence.org/v1alpha1"
	kind:       "ForgejoInstance"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		configPath:      #AbsolutePath
		workDir:         #AbsolutePath
		dataDir:         #AbsolutePath
		logDir:          #AbsolutePath
		repositoriesDir: #AbsolutePath
		reportPath:      #AbsolutePath
		user:            #NonEmptyString
		group:           #NonEmptyString
		server: {
			httpAddr: #Host
			httpPort: int & >=1 & <=65535
			domain:   #DNSName
			rootURL:  #HTTPURL
		}
		openBao: {
			address: #HTTPURL
			caCert:  #AbsolutePath
			secretKeyRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
			internalTokenRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
			lfsJWTSecretRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
			oauthJWTSecretRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
			automationTokenRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
		}
	}
}
