export type NaveeAperture = "fullscreen" | "dock" | "hidden";

export type NaveeMode = "expanded" | "docked" | "attention" | "speaking" | "hidden";

export type NaveeTone = "idle" | "good" | "warn" | "danger";

export type NaveeRoutePosture = "expanded" | "docked" | "hidden";

export type NaveeMessage = {
  readonly id: string;
  readonly text: string;
  readonly tone: NaveeTone;
};

export type NaveeSpeech =
  | { readonly kind: "idle" }
  | { readonly kind: "speaking"; readonly messageId: string };

export type NaveeState = {
  readonly activeMessage: NaveeMessage;
  readonly attentionCount: number;
  readonly queue: readonly NaveeMessage[];
  readonly speech: NaveeSpeech;
};

export type NaveeEvent =
  | { readonly type: "dock.clicked" }
  | { readonly type: "message.dismissed"; readonly id: string }
  | { readonly type: "message.received"; readonly message: NaveeMessage }
  | { readonly type: "speech.finished" }
  | { readonly type: "speech.started"; readonly messageId: string };

export type NaveeSnapshot = {
  readonly aperture: NaveeAperture;
  readonly mode: NaveeMode;
  readonly message: string;
  readonly tone: NaveeTone;
};

const DEFAULT_MESSAGE: NaveeMessage = {
  id: "for-you",
  text: "For You",
  tone: "idle",
};

export const initialNaveeState: NaveeState = {
  activeMessage: DEFAULT_MESSAGE,
  attentionCount: 0,
  queue: [],
  speech: { kind: "idle" },
};

export function naveeReducer(state: NaveeState, event: NaveeEvent): NaveeState {
  switch (event.type) {
    case "dock.clicked":
      return state.attentionCount === 0 ? state : { ...state, attentionCount: 0 };
    case "message.dismissed":
      return dismissMessage(state, event.id);
    case "message.received":
      return receiveMessage(state, event.message);
    case "speech.finished":
      return { ...state, speech: { kind: "idle" } };
    case "speech.started":
      return { ...state, speech: { kind: "speaking", messageId: event.messageId } };
  }
}

export function resolveNaveeSnapshot(input: {
  readonly routePosture: NaveeRoutePosture;
  readonly state: NaveeState;
}): NaveeSnapshot {
  const { routePosture, state } = input;
  if (routePosture === "hidden") {
    return {
      aperture: "hidden",
      message: state.activeMessage.text,
      mode: "hidden",
      tone: state.activeMessage.tone,
    };
  }
  if (routePosture === "expanded") {
    return {
      aperture: "fullscreen",
      message: state.activeMessage.text,
      mode: "expanded",
      tone: state.activeMessage.tone,
    };
  }
  if (state.speech.kind === "speaking") {
    return {
      aperture: "dock",
      message: state.activeMessage.text,
      mode: "speaking",
      tone: state.activeMessage.tone,
    };
  }
  if (state.attentionCount > 0) {
    return {
      aperture: "dock",
      message: state.activeMessage.text,
      mode: "attention",
      tone: state.activeMessage.tone,
    };
  }
  return {
    aperture: "dock",
    message: state.activeMessage.text,
    mode: "docked",
    tone: state.activeMessage.tone,
  };
}

function receiveMessage(state: NaveeState, message: NaveeMessage): NaveeState {
  const nextQueue = [...state.queue, message];
  return {
    ...state,
    activeMessage: message,
    attentionCount: state.attentionCount + 1,
    queue: nextQueue,
  };
}

function dismissMessage(state: NaveeState, id: string): NaveeState {
  const queue = state.queue.filter((message) => message.id !== id);
  const activeMessage =
    state.activeMessage.id === id ? (queue[0] ?? DEFAULT_MESSAGE) : state.activeMessage;
  return {
    ...state,
    activeMessage,
    attentionCount: Math.max(0, state.attentionCount - 1),
    queue,
    speech:
      state.speech.kind === "speaking" && state.speech.messageId === id
        ? { kind: "idle" }
        : state.speech,
  };
}
