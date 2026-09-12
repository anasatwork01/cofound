import type { Metadata } from "next"
import { PageHeader } from "@/components/page-header"
import { SignInForm } from "./sign-in-form"

export const metadata: Metadata = { title: "Sign in" }

/**
 * The door. SPEC §18's route table starts on the other side of it — every
 * screen it lists assumes a signed-in reader — so this route is added
 * deliberately rather than found there. `@/lib/routes` says so at length.
 *
 * One column, narrow, nothing else on the screen: there is exactly one thing to
 * do here and no navigation that would work before you have done it.
 */
export default function SignInPage() {
  return (
    <div className="mx-auto w-full max-w-md space-y-8 px-5 py-16">
      <PageHeader
        title="Sign in"
        lede="Type your email and we send you a link. There is no password, and if you have not used Halyard before the link makes your account."
      />
      <SignInForm />
    </div>
  )
}
