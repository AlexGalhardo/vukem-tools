import { useId, useRef, useState } from "react";

import Icon from "./Icon";

interface Props {
  /** accept is the MIME types this tool reads, for the file picker's filter.
   *  It is a hint, not a check: the server identifies the bytes. */
  accept: string[];
  /** onFiles receives what the person chose, by drop or by picker. */
  onFiles: (files: File[]) => void;
  /** maxBytes is shown so the limit is known before a 500 MB upload fails. */
  maxBytes: number;
  disabled?: boolean;
}

/**
 * The drop zone.
 *
 * It is a real `<input type="file">` with a label over it, not a div with
 * click handlers: that gets the keyboard, the picker and the screen-reader
 * announcement for free, and they are the things a hand-rolled drop zone
 * always gets wrong. Dropping is an addition on top, for the pointer.
 *
 * The drag counter exists because `dragleave` fires when the pointer crosses
 * into a child element, so tracking a boolean makes the highlight flicker as
 * the pointer moves over the icon and the text.
 */
export default function DropZone({ accept, onFiles, maxBytes, disabled }: Props): React.ReactElement {
  const [dragging, setDragging] = useState(false);
  const depth = useRef(0);
  const id = useId();

  const take = (list: FileList | null) => {
    const files = Array.from(list ?? []);

    if (files.length > 0) {
      onFiles(files);
    }
  };

  return (
    <div
      onDragEnter={(e) => {
        e.preventDefault();
        depth.current += 1;
        setDragging(true);
      }}
      onDragOver={(e) => e.preventDefault()}
      onDragLeave={() => {
        depth.current -= 1;

        if (depth.current <= 0) {
          depth.current = 0;
          setDragging(false);
        }
      }}
      onDrop={(e) => {
        e.preventDefault();
        depth.current = 0;
        setDragging(false);

        if (!disabled) {
          take(e.dataTransfer.files);
        }
      }}
      className={`rounded-panel border-2 border-dashed transition-colors duration-100 ${
        dragging ? "border-signal bg-signal-soft" : "border-edge bg-raised"
      } ${disabled ? "opacity-60" : ""}`}
    >
      <label
        htmlFor={id}
        className={`flex flex-col items-center gap-2 px-6 py-12 text-center ${
          disabled ? "cursor-not-allowed" : "cursor-pointer"
        }`}
      >
        <Icon name="upload" className={`size-7 ${dragging ? "text-signal" : "text-ink-faint"}`} />
        <span className="font-medium">
          Drop files here, or <span className="text-signal">choose them</span>
        </span>
        <span className="text-sm text-ink-soft">
          Up to {Math.round(maxBytes / (1024 * 1024))} MB each. Results are deleted after 5 minutes.
        </span>
      </label>
      <input
        id={id}
        type="file"
        multiple
        accept={accept.join(",")}
        disabled={disabled}
        onChange={(e) => {
          take(e.target.files);
          // Cleared so choosing the same file twice in a row still fires a
          // change event, which is the obvious thing to do after a failure.
          e.target.value = "";
        }}
        className="sr-only"
      />
    </div>
  );
}
