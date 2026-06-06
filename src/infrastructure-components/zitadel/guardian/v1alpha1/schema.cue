package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#DNSName:        =~"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$"
#HTTPURL:        =~"^https?://[^\\s]+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#ZitadelCluster: {
	apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
	kind:       "ZitadelCluster"
	metadata:   #Metadata
	spec: {
		externalDomain:     #DNSName
		runtimeArtifact:    #RepoPath
		runtimeRoot:        #AbsolutePath
		configPath:         #AbsolutePath
		stepsPath:          #AbsolutePath
		adminPATPath:       #AbsolutePath
		discoveryHostsPath: #AbsolutePath
		user:               #NonEmptyString
		group:              #NonEmptyString
		openBao: {
			address:            #HTTPURL
			caCert:             #AbsolutePath
			masterkeySecretRef: #ObjectRef
			adminPasswordRef:   #ObjectRef
			adminPATSecretRefs: [...#ObjectRef]
			smtpPasswordRef: #ObjectRef
		}
		instance: {
			name:    #NonEmptyString
			orgName: #NonEmptyString
			human: {
				userName:  #NonEmptyString
				firstName: #NonEmptyString
				lastName:  #NonEmptyString
				email:     #NonEmptyString
			}
			machine: {
				userName: #NonEmptyString
				name:     #NonEmptyString
			}
		}
		smtp: {
			host:     #NonEmptyString
			user:     #NonEmptyString
			from:     #NonEmptyString
			fromName: #NonEmptyString
		}
	}
}

#ZitadelAuthControlPlane: {
	apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
	kind:       "ZitadelAuthControlPlane"
	metadata:   #Metadata
	spec: {
		zitadelBaseURL:   #HTTPURL
		zitadelHost:      #DNSName
		verselfDomain:    #DNSName
		iamServiceDomain: #DNSName
		projectName:      #NonEmptyString
		browserAppName:   #NonEmptyString
		cliAppName:       #NonEmptyString
		claimsTargetName: #NonEmptyString
		claimsActionPath: #AbsolutePath
		openBao: {
			address:                     #HTTPURL
			caCert:                      #AbsolutePath
			adminPATSecretRef:           #ObjectRef
			githubLoginClientID:         string
			githubLoginClientSecretRef?: #ObjectRef
		}
	}
}
