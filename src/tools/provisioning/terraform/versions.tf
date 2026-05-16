terraform {
  required_version = ">= 1.6.0"
  required_providers {
    latitudesh = {
      source  = "latitudesh/latitudesh"
      version = "~> 2.5"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.19"
    }
  }
}
