# Motion and interactive feedback

Werkbord keeps its existing layouts, icons, typography and material. Motion connects an action to its observed result; it never delays a mutation or supplies evidence that the controller or runner has not reported.

## Salie

Newly appended text fades over 120 ms while earlier text stays still. Text becomes available immediately, without a typewriter queue. Completed Markdown blocks, code and generated forms retain their existing behaviour. Temporary fragment spans are compacted after the animation when doing so cannot disturb a selection. Restored or replaced text does not replay an entrance.

The activity line uses the existing pixel mark at 24 px. Replies cycle through the six approved studies: DNA helix, travelling wave, orbital braid, infinity current, pixel relay and domino cascade. One style stays fixed for a reply, including after reopening; breathing fan and equaliser are excluded. The 48 original cells and theme ramps remain intact. The loop is capped at 30 fps, begins with the original mark and settles back when Stop is pending. Its line can still be opened to inspect reported operations. Animation frames stop outside the viewport or in a hidden document; reduced motion restores the static mark immediately. A Stop request remains pending until a terminal event or reconciled session confirms its outcome. Existing partial text remains available after cancellation or failure.

The conversation follows its tail through animation-frame-coalesced scroll updates. Scrolling up or selecting text preserves the reading position. **New response below** returns to the tail over 180 ms; scrolling interrupts that travel. Existing keyboard, dictation, attachment and approval boundaries remain in place.

## Work and review

The shared product-neutral motion asset provides card continuity to both Individual and Team. Confirmed cross-column moves travel over 240 ms; neighbours reflow over 200 ms; newly saved cards arrive over 180 ms. Initial loads, reconnect baselines, scrolling, resizing, hidden documents and reduced motion do not replay those moves. Presentation-only cross-column copies are inert, inaccessible and removed on completion or cancellation.

Question delivery keeps its existing pending and uncertain states. A confirmed answer can resolve locally while the attention list closes its gap. Review assessments, confirmed Git results and saved Done states receive small local acknowledgements. An accepted assessment, local merge, remote confirmation and completed task remain separate outcomes.

On the phone, a saved column change offers **View in Review/Done** through the existing notice action. Task details and sheets use 220 ms entrances and 180 ms exits while preserving their routes, focus restoration, list position and existing geometry. Planned-date changes use the saved dates rather than implying an agent start.

## Transfers

Git Fetch and the confirmed Push sheet retain their existing API and approval paths. A bounded downward/upward arrow and thin indeterminate current acknowledge the pending request. No percentage or intermediate network stage is inferred. A successful fetch briefly reads Fetched before returning to Fetch; the existing result remains available. Only a successful result with any remote verification satisfied receives a drawn check. Unconfirmed remote outcomes use amber and retain their evidence; errors and refusals keep their own outcomes. No automatic retry is introduced.

Attachment sharing still requires the existing per-file approval before any upload. Each chip carries an indeterminate current only while uploading. A newly accepted file receives a 160 ms check and local acknowledgement, then returns to its attachment icon. Already-ready files do not replay acknowledgement on reopening. Send remains unavailable while any upload is pending. A failed file keeps its error and remove action; the draft and ready attachments remain available. Removing a queued file skips its upload; a file accepted after removal is released through the existing detach API. Removing a file does not claim an in-flight request was cancelled.

Transfer loops pause offscreen and in hidden documents, including pseudo-elements. Reduced motion keeps static direction/status icons, labels and result evidence without the moving current or check drawing. The same controls work on desktop and phone, and file removal has a 44 px touch target.

## Analytics

Labels, qualifiers and exact numbers are available immediately. Activity bars grow together from their baseline over 480 ms; comparison bars extend over 420 ms. Compatible user-selected scopes transition over 280 ms. Background snapshots update quietly instead of resetting the charts to zero.

Individual's daily chart supports pointer inspection, phone taps and a keyboard date slider: Left/Right or Up/Down changes the selected date, Home/End chooses an endpoint and Escape clears inspection. The exact-value readout is an overlay, so it does not move the surrounding layout. Daily values remain available as a table. Team retains its own coordination figures and values table.

## Phone and setup

Deck keys keep their existing icons and tactile press. Swaps animate between their actual sockets, configuration uses the existing sheet, and a successful save acknowledges the existing edit control. Tap, hold, drag and approval retain their existing execution semantics. The completed guide withdraws without adding another screen. Onboarding can animate the existing visible mark once after completion is saved.

Workflow motion reflects reported stage and result changes. The banner can resolve to its observed terminal state, checks acknowledge newly reported stages, and saved findings remain drafts for review. Loading old history does not recreate past accomplishments. Team phone access remains a read-only summary.

## Accessibility and checks

Every new spatial effect has a reduced-motion path. Text, exact values, status, focus and recovery remain available. Visual loops stop when hidden; controller work continues independently. No new animation dependency is required.

`make check` covers the unit, type, lint, architecture and build checks. `make test-motion-browser` checks real saved board transitions, initial-load baselines, all six streaming studies, offscreen/reduced-motion behaviour, real Git fetch/push results, partial attachment readiness and recovery, Stop acknowledgement, chart inspection on desktop/phone, reduced motion and Team coordination against disposable data. The existing Assistant, phone, planning and analytics browser suites cover the surrounding workflows.
