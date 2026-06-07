package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s]+$"
#HTTPURL:        =~"^https?://[^\\s]+$"
#AbsolutePath:   =~"^/[^\\s]*$"

#Metadata: {
	name: #NonEmptyString
}

#DeploymentService: {
	apiVersion: "deployment.guardianintelligence.org/v1alpha1"
	kind:       "DeploymentService"
	metadata:   #Metadata
	spec: {
		site:          #NonEmptyString
		publicBaseURL: #HTTPSURL
		sourceRepo: {
			root:        #AbsolutePath
			url:         #HTTPSURL
			initTimeout: #NonEmptyString
		}
		nomad: {
			address: #HTTPURL
		}
		objectStorage: {
			address: #HTTPSURL
		}
		postgres: {
			dsn:      #NonEmptyString
			maxConns: int & >0
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
		bazel: {
			jobs: int & >0
		}
		admissionConcurrency: int & >0
		recoverySSHReady:     bool
		githubOIDC?: {
			allowedRepositories: #NonEmptyString
			allowedRefs:         #NonEmptyString
			allowedWorkflowRefs: #NonEmptyString
		}
	}
}
