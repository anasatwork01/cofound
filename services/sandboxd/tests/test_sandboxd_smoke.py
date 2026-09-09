from halyard_sandboxd import SERVICE
from halyard_sandboxd.__main__ import main


def test_service_name() -> None:
    """The service name is a metric label and an OTel service.name."""
    assert SERVICE == "sandboxd"


def test_version_flag_exits_zero() -> None:
    assert main(["--version"]) == 0
