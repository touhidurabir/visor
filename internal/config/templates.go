package config

// Template is a starting point for a new program config. Values holds the
// form fields ("name" is the program name) as they appear in the form.
type Template struct {
	ID, Label string
	Values    map[string]string
}

// FormKeys are the settings the form shows, in the order they are written.
var FormKeys = []string{
	"command", "directory", "user", "numprocs", "process_name",
	"autostart", "autorestart", "startsecs", "startretries",
	"stopwaitsecs", "stopsignal", "stopasgroup", "killasgroup",
	"redirect_stderr", "stdout_logfile", "stderr_logfile", "environment", "priority",
}

// Templates returns the built-in starting points: a Laravel queue worker,
// Horizon, the scheduler, a Node app and any script. Paths under /srv/acme
// are placeholders the person is expected to replace.
func Templates() []Template {
	mk := func(id, label string, over map[string]string) Template {
		v := map[string]string{
			"autostart": "true", "autorestart": "true", "startsecs": "1", "startretries": "3",
			"stopsignal": "TERM", "stopasgroup": "true", "killasgroup": "true",
			"redirect_stderr": "true", "priority": "999", "numprocs": "1", "process_name": "%(program_name)s",
		}
		for k, x := range over {
			v[k] = x
		}
		return Template{ID: id, Label: label, Values: v}
	}
	return []Template{
		mk("queue", "Laravel queue worker", map[string]string{
			"name": "acme-queue", "command": "php artisan queue:work --sleep=3 --tries=3 --max-time=3600",
			"directory": "/srv/acme", "numprocs": "3", "process_name": "%(program_name)s_%(process_num)02d",
			"stopwaitsecs": "3600", "stdout_logfile": "/var/log/acme/queue.log",
		}),
		mk("horizon", "Laravel Horizon", map[string]string{
			"name": "acme-horizon", "command": "php artisan horizon", "directory": "/srv/acme",
			"stopwaitsecs": "3600", "stdout_logfile": "/srv/acme/storage/logs/horizon.log",
		}),
		mk("scheduler", "Laravel scheduler", map[string]string{
			"name": "acme-scheduler", "command": "php artisan schedule:work", "directory": "/srv/acme",
			"stopwaitsecs": "60", "stdout_logfile": "/srv/acme/storage/logs/scheduler.log",
		}),
		mk("node", "Node app", map[string]string{
			"name": "acme-web", "command": "node server.js", "directory": "/srv/acme-web",
			"stopwaitsecs": "10", "environment": `NODE_ENV="production",PORT="3000"`,
			"stdout_logfile": "/srv/acme-web/logs/app.log",
		}),
		mk("script", "Any script", map[string]string{
			"name": "acme-task", "command": "/srv/acme/bin/run.sh", "directory": "/srv/acme",
			"autorestart": "unexpected", "stopwaitsecs": "10",
		}),
	}
}
