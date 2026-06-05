package v1alpha1

#CloudflareRecovery: {
	apiVersion: "recovery.verself.sh/v1alpha1"
	kind:       "CloudflareRecovery"
	metadata: name: =~"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	spec: {
		site:                  =~"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
		accountID:             =~"^[a-f0-9]{32}$"
		accountAdminTokenFile: =~"^/.*"
		targetIPv4:            =~"^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$"
		openBao: {
			address:   =~"^https?://.+"
			tokenFile: =~"^(/|\\$\\{).+"
			caCertFile?: =~"^(/|\\$\\{).+"
		}
		dns: {
			zones: [...{
				name:   =~"^[a-z][a-z0-9-]*$"
				zone:   =~"^[A-Za-z0-9_.-]+$"
				domain: =~"^[A-Za-z0-9_.-]+$"
			}]
			records: [...{
				zone:    =~"^[a-z][a-z0-9-]*$"
				record:  string
				ttl:     int & >=1
				proxied: bool
			}]
		}
		tls: {
			outputDir: =~"^/.*"
			acme: {
				directoryURL:       =~"^https://.+"
				contactEmail:       =~"^.+@.+$"
				dnsPropagationWait: string
				renewBefore:        string
			}
			certificates: [...{
				name:    string
				domains: [...string]
			}]
		}
		objectStorage: {
			bucket:        =~"^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$"
			childTokenTTL: string
			runtimeSecrets: {
				adminAccessKeyID:     string
				adminSecretAccessKey: string
				proxyAccessKeyID:     string
				proxySecretAccessKey: string
			}
		}
	}
}
