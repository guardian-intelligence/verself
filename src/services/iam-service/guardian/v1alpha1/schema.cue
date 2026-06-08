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

#IAMService: {
	apiVersion: "iam.guardianintelligence.org/v1alpha1"
	kind:       "IAMService"
	metadata:   #Metadata
	spec: {
		installationID: #NonEmptyString
		auth: {
			issuerURL:                   #HTTPSURL
			browserAuthPublicBaseURL:    #HTTPSURL
			pwnedPasswordsRangeEndpoint: #HTTPSURL
		}
		zitadel: {
			host: #NonEmptyString
		}
		postgres: {
			dsn:      #NonEmptyString
			maxConns: int & >0
		}
		clickhouse: {
			address:    #NonEmptyString
			user:       #NonEmptyString
			caCertPath: #AbsolutePath
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
		credentials: {
			zitadelActionSigningKeyRef: #CredentialRef
			browserOIDCClientIDRef:     #CredentialRef
			browserOIDCClientSecretRef: #CredentialRef
			githubLoginIDPRef?:         #CredentialRef
			authAudienceRef:            #CredentialRef
			zitadelAdminTokenRef:       #CredentialRef
			spiceDBPresharedKeyRef:     #CredentialRef
			emailIdentityHMACKeyRef:    #CredentialRef
		}
	}
}
