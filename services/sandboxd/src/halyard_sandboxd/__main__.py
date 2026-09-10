"""``python -m halyard_sandboxd``.

Short on purpose. Everything a service shares lives in the chassis; what is
here is what only sandboxd knows.
"""

from __future__ import annotations

import sys

from halyard_chassis import Service, Setup, run

from . import SERVICE, __version__
from .probes import PostgresProbe, RedisProbe
from .settings import SandboxdSettings


def build(setup: Setup) -> None:
    """Register what this service depends on and serves.

    Task 1.3 adds the routes, defined by
    packages/schema/sandboxd.openapi.yaml. Until then this is the chassis with
    probes attached, which is a real deployable: it reports its readiness
    honestly and shuts down cleanly.
    """
    settings = setup.settings
    if settings.database_url:
        postgres = PostgresProbe(settings.database_url)
        setup.registry.register("postgres", postgres.check)
        setup.shutdown.on_close("postgres-probe", postgres.close)
    if settings.redis_url:
        redis = RedisProbe(settings.redis_url)
        setup.registry.register("redis", redis.check)
        setup.shutdown.on_close("redis-probe", redis.close)


def main(argv: list[str] | None = None) -> int:
    return run(
        Service(
            name=SERVICE,
            version=__version__,
            settings_class=SandboxdSettings,
            build=build,
        ),
        argv=argv,
    )


if __name__ == "__main__":
    sys.exit(main())
