job "electric-containerd" {
  name = "electric-containerd"
  datacenters = ["dc1"]
  type = "service"

  group "containerd" {
    count = 1

    task "containerd" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "45s"

      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "rm -f /run/electric-containerd/containerd.sock\ninstall -d -m 0755 /run/electric-containerd/state /var/lib/electric-containerd/root\nexec env PATH=\"$PWD/local/bin:$PATH\" local/bin/containerd \\\n  --address /run/electric-containerd/containerd.sock \\\n  --root /var/lib/electric-containerd/root \\\n  --state /run/electric-containerd/state\n"]
      }

      resources {
        cpu = 200
        memory = 256
      }

      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
    }

    update {
      max_parallel = 1
      health_check = "task_states"
      min_healthy_time = "3s"
      healthy_deadline = "300s"
      progress_deadline = "600s"
      auto_revert = true
    }
  }
}
