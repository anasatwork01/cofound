"""sandboxd's configuration: the chassis fields plus its own.

Extending rather than redefining is what keeps one deployment manifest valid
for every service. Note the explicit aliases: the variable names are a contract
shared with the Go services, so they are written down rather than derived from
field names.
"""

from __future__ import annotations

from typing import Annotated, ClassVar

from halyard_chassis import ChassisSettings
from pydantic import Field


class SandboxdSettings(ChassisSettings):
    """SPEC 3.2 names asyncpg for the Python services and SPEC 3.3 names Redis.

    Both are empty by default and both are checked by a readiness probe when
    set, so a local run needs neither -- but a deployment that forgets one
    reports unready rather than failing on the first request that needed it.
    """

    # SettingsConfigDict merges across the MRO, so anything not restated here
    # keeps the base's value. Nothing needs changing, which is why there is no
    # model_config at all rather than a copied one that could drift.

    database_url: Annotated[str, Field(validation_alias="DATABASE_URL")] = ""
    redis_url: Annotated[str, Field(validation_alias="REDIS_URL")] = ""

    modal_token_id: Annotated[str, Field(validation_alias="MODAL_TOKEN_ID")] = ""
    modal_token_secret: Annotated[str, Field(validation_alias="MODAL_TOKEN_SECRET")] = ""

    # Fingerprinted in the boot line rather than printed. DATABASE_URL and
    # REDIS_URL carry a password inline, which is also why they are on the
    # credential-prefix list the log redactor scans for.
    secret_env: ClassVar[frozenset[str]] = ChassisSettings.secret_env | {
        "DATABASE_URL",
        "REDIS_URL",
        "MODAL_TOKEN_SECRET",
    }

    owned_prefixes: ClassVar[tuple[str, ...]] = (
        *ChassisSettings.owned_prefixes,
        "DATABASE_",
        "REDIS_",
        "MODAL_",
    )
