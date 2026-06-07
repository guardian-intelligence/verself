package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s]+$"
#AbsolutePath:   =~"^/[^\\s]*$"

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

#SecretsService: {
	apiVersion: "secrets.guardianintelligence.org/v1alpha1"
	kind:       "SecretsService"
	metadata:   #Metadata
	spec: {
		installationID: #NonEmptyString
		environment:    #NonEmptyString
		auth: {
			issuerURL:   #HTTPSURL
			audienceRef: #CredentialRef
		}
		openbao: {
			address:          #HTTPSURL
			caCertPath:       #AbsolutePath
			kvPrefix:         #NonEmptyString
			transitPrefix:    #NonEmptyString
			jwtPrefix:        #NonEmptyString
			spiffeJWTPrefix:  #NonEmptyString
			workloadAudience: #NonEmptyString
		}
		runtimeSecretNamespace: #NonEmptyString
		spiffe: {
			endpointSocket: #NonEmptyString
		}
	}
}
