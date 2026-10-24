import { describe, expect, it } from 'vitest';
import { fileKey, previewKind, sizeLabel, stem, typeOf } from './files';

describe('files view helpers', () => {
  it('decides how a file is shown', () => {
    expect(previewKind('application/pdf')).toBe('pdf');
    expect(previewKind('image/png')).toBe('image');
    expect(previewKind('video/mp4')).toBe('video');
    expect(previewKind('audio/mpeg')).toBe('audio');
    expect(previewKind('text/markdown')).toBe('markdown');
    expect(previewKind('application/octet-stream', 'notes.md')).toBe('markdown');
    expect(previewKind('text/plain')).toBe('text');
    expect(previewKind('application/json')).toBe('text');
    expect(previewKind('application/zip')).toBe('none');
  });
  it('shows web pages as the text they are and never runs them', () => {
    expect(typeOf('page.html', 'text/html')).toBe('text/plain');
    expect(typeOf('page.htm', '')).toBe('text/plain');
    expect(typeOf('anything', 'text/html')).toBe('text/plain');
    expect(previewKind(typeOf('page.html', 'text/html'))).toBe('text');
  });
  it('falls back to the name when the browser does not know the type', () => {
    expect(typeOf('brochure.PDF', '')).toBe('application/pdf');
    expect(typeOf('brochure.pdf', 'application/octet-stream')).toBe('application/pdf');
    expect(typeOf('notes.md', '')).toBe('text/markdown');
    expect(typeOf('data.bin', '')).toBe('application/octet-stream');
    expect(typeOf('photo.jpg', 'image/jpeg')).toBe('image/jpeg');
  });
  it('writes sizes for people', () => {
    expect(sizeLabel(900)).toBe('900 B');
    expect(sizeLabel(2048)).toBe('2.0 KB');
    expect(sizeLabel(5 * 1024 * 1024)).toBe('5.0 MB');
    expect(sizeLabel(40 * 1024 * 1024)).toBe('40 MB');
  });
  it('names an entry after its file', () => {
    expect(stem('Spring_campaign_v2.pdf')).toBe('Spring campaign v2');
    expect(stem('.gitignore')).toBe('.gitignore');
    expect(stem('noextension')).toBe('noextension');
  });
  it('keeps each workspace’s files apart', () => {
    expect(fileKey('pws_a', 'prec_b')).toBe('pws_a/prec_b');
  });
});
