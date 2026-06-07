package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#HTTPURL:        =~"^https?://[^\\s]+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#OpenBaoCluster: {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "OpenBaoCluster"
	metadata:   #Metadata
	spec: {
		address:          #HTTPURL
		caCert:           #AbsolutePath
		runtimeRoot:      #AbsolutePath
		dataDir:          #AbsolutePath
		configPath:       #AbsolutePath
		reportPath:       #AbsolutePath
		initMaterialPath: #AbsolutePath
		seal: {
			shamir: {
				keyShares:    int & >0
				keyThreshold: int & >0 & <=keyShares
				pgpRecipientRefs: [#ObjectRef & {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "PGPRecipient"
				}, ...]
			}
		}
		snapshots?: {
			restore?: {
				manifestPath?: #AbsolutePath
				snapshotPath?: #AbsolutePath
				sourceRef?:    #ObjectRef
			}
			save?: {
				manifestPath:    #AbsolutePath
				snapshotPath:    #AbsolutePath
				destinationRef?: #ObjectRef
			}
		}
	}
}

#SecretPath: {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "SecretPath"
	metadata:   #Metadata
	spec: {
		path:   #NonEmptyString
		key:    #NonEmptyString
		source: "generated" | "producedBy" | "operatorImport"
		if source == "generated" {
			generate: {
				bytes:    int & >0
				encoding: "hex" | "base64url" | "alphanumeric" | "password"
			}
		}
		if source == "producedBy" {
			producerRef: #ObjectRef
		}
	}
}

#PGPRecipient: {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "PGPRecipient"
	metadata:   #Metadata
	spec: {
		publicKeyBase64: #NonEmptyString
	}
}
