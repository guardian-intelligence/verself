package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s]+$"
#HTTPURL:        =~"^https?://[^\\s]+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#CredentialRef: #ObjectRef & {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "SecretPath"
}

#SourceCodeHostingService: {
	apiVersion: "source.guardianintelligence.org/v1alpha1"
	kind:       "SourceCodeHostingService"
	metadata:   #Metadata
	spec: {
		installationID: #NonEmptyString
		publicBaseURL:  #HTTPSURL
		auth: {
			issuerURL:   #HTTPSURL
			audienceRef: #CredentialRef
		}
		forgejo: {
			baseURL:            #HTTPURL
			owner:              #NonEmptyString
			automationTokenRef: #CredentialRef
			webhookSecretRef:   #CredentialRef
		}
		postgres: {
			dsn:      #NonEmptyString
			maxConns: int & >0
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
	}
}
