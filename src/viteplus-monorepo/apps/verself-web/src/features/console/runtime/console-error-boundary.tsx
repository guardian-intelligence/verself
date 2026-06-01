import { Component, type ErrorInfo, type ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import {
  CONSOLE_APP_SURFACE_RADIUS_CLASS,
  CONSOLE_PRIMARY_BACKGROUND_HEX,
  CONSOLE_PRIMARY_TEXT_HEX,
} from "./console-layout";

type ConsoleAppBoundaryProps = {
  readonly appLabel: string;
  readonly children: ReactNode;
  readonly resetKey: string;
};

type ConsoleAppBoundaryState = {
  readonly error: Error | null;
};

export class ConsoleAppBoundary extends Component<
  ConsoleAppBoundaryProps,
  ConsoleAppBoundaryState
> {
  override state: ConsoleAppBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ConsoleAppBoundaryState {
    return { error };
  }

  override componentDidCatch(_error: Error, _errorInfo: ErrorInfo) {
    // Browser runtime reporting already captures uncaught errors globally.
  }

  override componentDidUpdate(previousProps: ConsoleAppBoundaryProps) {
    if (previousProps.resetKey !== this.props.resetKey && this.state.error) {
      this.setState({ error: null });
    }
  }

  override render() {
    if (!this.state.error) return this.props.children;
    return (
      <div
        className={`flex h-full flex-col overflow-hidden px-7 py-7 ${CONSOLE_APP_SURFACE_RADIUS_CLASS}`}
        style={{
          backgroundColor: CONSOLE_PRIMARY_BACKGROUND_HEX,
          color: CONSOLE_PRIMARY_TEXT_HEX,
        }}
      >
        <div className="flex size-10 items-center justify-center rounded-full border border-[#ff8f7e]/18 bg-[#ff8f7e]/12 text-[#ffb4a8]">
          <AlertTriangle className="size-4" aria-hidden="true" />
        </div>
        <h1 className="mt-5 max-w-60 text-[1.55rem] font-semibold leading-[1.03] tracking-normal">
          {this.props.appLabel} could not render
        </h1>
        <p className="mt-4 max-w-64 text-sm font-medium leading-5 text-white/52">
          The shell is still available. Reset this app panel or switch to another surface.
        </p>
        <button
          className="mt-auto inline-flex h-10 w-fit items-center justify-center rounded-full bg-white px-5 text-sm font-semibold text-black transition hover:bg-white/88 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
          onClick={() => this.setState({ error: null })}
          type="button"
        >
          Reset panel
        </button>
      </div>
    );
  }
}
