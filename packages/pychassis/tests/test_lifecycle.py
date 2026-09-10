"""The ordered stop, and the drain signal ASGI does not provide."""

from __future__ import annotations

import asyncio
import signal

from halyard_chassis.lifecycle import Shutdown, ShutdownConfig


async def test_the_drain_signal_is_set_before_the_server_is_told() -> None:
    """The whole reason this class exists.

    uvicorn never tells an in-flight handler the SERVER is stopping, and its
    lifespan shutdown does not run until in-flight work finishes -- so a live
    stream could only learn about shutdown after it had already ended. Chaining
    in front of uvicorn's handler is what breaks that circle.
    """
    handed_over = asyncio.Event()
    drain_was_set_at_handover = False

    s = Shutdown(ShutdownConfig(deregister_delay=0.0, drain_timeout=0.01))

    def on_ready() -> None:
        nonlocal drain_was_set_at_handover
        drain_was_set_at_handover = s.is_draining
        handed_over.set()

    s.install(on_ready_to_stop=on_ready)
    try:
        signal.raise_signal(signal.SIGTERM)
        async with asyncio.timeout(2):
            await handed_over.wait()
        assert drain_was_set_at_handover, (
            "the server was told to stop before streams were told to drain"
        )
    finally:
        s.restore()


async def test_a_stream_can_wind_itself_down_during_the_drain_window() -> None:
    """The shape every long-lived response must use."""
    s = Shutdown(ShutdownConfig(deregister_delay=0.0, drain_timeout=0.2))
    closed_cleanly = False

    async def stream() -> None:
        nonlocal closed_cleanly
        while True:
            _done, pending = await asyncio.wait(
                [
                    asyncio.ensure_future(asyncio.sleep(10)),
                    asyncio.ensure_future(s.draining.wait()),
                ],
                return_when=asyncio.FIRST_COMPLETED,
            )
            for task in pending:
                task.cancel()
            if s.is_draining:
                closed_cleanly = True
                return

    s.install(on_ready_to_stop=lambda: None)
    try:
        task = asyncio.create_task(stream())
        signal.raise_signal(signal.SIGTERM)
        async with asyncio.timeout(2):
            await task
        assert closed_cleanly
    finally:
        s.restore()


async def test_a_second_signal_is_absorbed() -> None:
    """Matches uvicorn: handle_exit promotes to force_exit only for SIGINT, so
    a supervisor re-sending SIGTERM to hurry a worker along does nothing."""
    calls = 0
    handed = asyncio.Event()

    def on_ready() -> None:
        nonlocal calls
        calls += 1
        handed.set()

    s = Shutdown(ShutdownConfig(deregister_delay=0.0, drain_timeout=0.01))
    s.install(on_ready_to_stop=on_ready)
    try:
        signal.raise_signal(signal.SIGTERM)
        signal.raise_signal(signal.SIGTERM)
        async with asyncio.timeout(2):
            await handed.wait()
        await asyncio.sleep(0.05)
        assert calls == 1
    finally:
        s.restore()


async def test_closers_run_in_reverse_registration_order() -> None:
    """A pool registered after the thing that uses it is torn down second."""
    order: list[str] = []
    s = Shutdown(ShutdownConfig(server_timeout=1.0))

    async def close(name: str) -> None:
        order.append(name)

    s.on_close("postgres", lambda: close("postgres"))
    s.on_close("redis", lambda: close("redis"))
    await s.close()
    assert order == ["redis", "postgres"]


async def test_a_closer_that_fails_does_not_stop_the_others() -> None:
    """Refusing to continue would leave later dependencies open on every
    shutdown where one of them was already broken."""
    closed: list[str] = []
    s = Shutdown(ShutdownConfig(server_timeout=1.0))

    async def boom() -> None:
        raise RuntimeError("already closed")

    async def fine() -> None:
        closed.append("redis")

    s.on_close("redis", fine)
    s.on_close("postgres", boom)
    await s.close()
    assert closed == ["redis"]


async def test_a_closer_that_hangs_is_abandoned_within_its_budget() -> None:
    """timeout_graceful_shutdown does NOT bound the lifespan block, so a hung
    pool close would otherwise hang the process regardless of every uvicorn
    setting."""
    s = Shutdown(ShutdownConfig(server_timeout=0.05))
    reached = False

    async def hangs() -> None:
        await asyncio.sleep(10)

    async def after() -> None:
        nonlocal reached
        reached = True

    s.on_close("after", after)
    s.on_close("hangs", hangs)
    async with asyncio.timeout(2):
        await s.close()
    assert reached, "a hung closer blocked the ones registered before it"


async def test_each_closer_gets_a_fresh_budget() -> None:
    """A cancellation delivered while a hook is inside a finally interrupts the
    cleanup mid-way, which is how a connection ends up neither released nor
    closed."""
    finished: list[str] = []
    s = Shutdown(ShutdownConfig(server_timeout=0.2))

    async def slow(name: str) -> None:
        await asyncio.sleep(0.1)
        finished.append(name)

    s.on_close("a", lambda: slow("a"))
    s.on_close("b", lambda: slow("b"))
    async with asyncio.timeout(2):
        await s.close()
    assert finished == ["b", "a"]


async def test_the_drain_phase_ends_immediately_with_no_open_streams() -> None:
    """A ceiling, not a fixed delay.

    The first version slept the whole window unconditionally, which put a
    ten-second pause on every restart of a service with nothing streaming --
    caught by running the service, not by a test, which is why this one exists.
    """
    handed = asyncio.Event()
    s = Shutdown(ShutdownConfig(deregister_delay=0.0, drain_timeout=30.0))
    s.install(on_ready_to_stop=handed.set)
    try:
        started = asyncio.get_running_loop().time()
        signal.raise_signal(signal.SIGTERM)
        async with asyncio.timeout(2):
            await handed.wait()
        assert asyncio.get_running_loop().time() - started < 1.0
    finally:
        s.restore()


async def test_the_drain_phase_waits_for_an_open_stream() -> None:
    handed = asyncio.Event()
    s = Shutdown(ShutdownConfig(deregister_delay=0.0, drain_timeout=5.0))

    async def stream() -> None:
        async with s.stream() as draining:
            await draining.wait()
            await asyncio.sleep(0.05)

    s.install(on_ready_to_stop=handed.set)
    try:
        task = asyncio.create_task(stream())
        await asyncio.sleep(0.01)
        assert s.open_streams == 1

        signal.raise_signal(signal.SIGTERM)
        async with asyncio.timeout(3):
            await task
            await handed.wait()
        assert s.open_streams == 0
    finally:
        s.restore()


async def test_the_drain_window_is_a_ceiling_not_a_promise() -> None:
    """A stream that ignores the signal does not hold shutdown open forever."""
    handed = asyncio.Event()
    s = Shutdown(ShutdownConfig(deregister_delay=0.0, drain_timeout=0.05))

    async def stubborn() -> None:
        async with s.stream():
            await asyncio.sleep(10)

    s.install(on_ready_to_stop=handed.set)
    try:
        task = asyncio.create_task(stubborn())
        await asyncio.sleep(0.01)
        signal.raise_signal(signal.SIGTERM)
        async with asyncio.timeout(2):
            await handed.wait()
        task.cancel()
    finally:
        s.restore()
