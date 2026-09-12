"use client"

/**
 * Who is signed in, as three honest states rather than two.
 *
 * `GET /auth/session` answers 200 with the user, or 401. TanStack Query calls
 * the 401 an error, which is true of the HTTP exchange and false of the
 * product: being signed out is a normal state of the console, not a fault. If
 * the console treated every failure as "signed out" it would sign a user out
 * every time their wifi dropped, and if it treated every failure as an error it
 * would show a red box to everyone who has never signed in.
 *
 * So the query's outcome is read into four cases, and a screen has to name the
 * one it is rendering:
 *
 *   loading      the answer is not back yet — render nothing that implies
 *                either outcome, because the wrong one flashing is worse than
 *                a beat of quiet
 *   signed-out   the API said 401
 *   signed-in    the API said 200
 *   unreachable  the request never got an answer, or got one nobody can read
 *
 * `unreachable` exists because the alternative is lying. A console served from
 * a CDN while the API is down would otherwise report "you are signed out",
 * which sends a signed-in user to re-enter their email and then fails there
 * too.
 */
import { useQuery, type UseQueryResult } from "@tanstack/react-query"
import { fetchSession, isUnauthenticated, queryKeys, type AuthSession } from "@/lib/api"

export type SessionState =
  | { readonly status: "loading" }
  | { readonly status: "signed-out" }
  | { readonly status: "signed-in"; readonly session: AuthSession }
  | { readonly status: "unreachable"; readonly error: unknown }

/**
 * The mapping, as a pure function so it can be exercised without a component.
 *
 * Takes the three fields it actually reads rather than the whole query object,
 * which keeps it callable from a test with a literal.
 */
export function readSessionState(query: {
  readonly isPending: boolean
  readonly data: AuthSession | undefined
  readonly error: unknown
}): SessionState {
  if (query.data !== undefined) return { status: "signed-in", session: query.data }
  if (query.isPending) return { status: "loading" }
  if (isUnauthenticated(query.error)) return { status: "signed-out" }
  // A pending query has no error and no data, so anything reaching here failed.
  return { status: "unreachable", error: query.error }
}

export type SessionQuery = UseQueryResult<AuthSession, unknown>

/**
 * The session, shared by every screen through one query key.
 *
 * One key means one request no matter how many components ask, and one
 * invalidation after a sign-in or an org creation corrects all of them.
 */
export function useSession(): SessionState & { readonly query: SessionQuery } {
  const query = useQuery({ queryKey: queryKeys.session, queryFn: fetchSession })
  return { ...readSessionState(query), query }
}
