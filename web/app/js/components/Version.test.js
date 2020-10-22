import React from 'react';
import Version from './Version.jsx';
import { mount } from 'enzyme';
import { i18nWrap } from '../../test/testHelpers.jsx';

describe('Version', () => {
  let curVer = "edge-1.2.3";
  let newVer = "edge-2.3.4";

  it('renders up to date message when versions match', () => {
    const component = mount(i18nWrap(
      <Version
        classes={{}}
        isLatest
        latestVersion={curVer}
        releaseVersion={curVer} />)
    );

    expect(component).toIncludeText("Linkerd is up to date");
  });

  it('renders update message when versions do not match', () => {
    const component = mount(i18nWrap(
      <Version
        classes={{}}
        isLatest={false}
        latestVersion={newVer}
        releaseVersion={curVer} />)
    );

    expect(component).toIncludeText("A new version (2.3.4) is available.");
  });

  it('renders error when version check fails', () => {
    let errMsg = "Fake error";

    const component = mount(i18nWrap(
      <Version
        classes={{}}
        error={{
          statusText: errMsg,
        }}
        isLatest={false}
        releaseVersion={curVer} />)
    );

    expect(component).toIncludeText("Version check failed: Fake error.");
    expect(component).toIncludeText(errMsg);
  });
});
