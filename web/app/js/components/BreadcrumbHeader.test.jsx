import { BrowserRouter } from 'react-router-dom';
import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import React from 'react';
import { mount } from 'enzyme';

const loc = {
  pathname: '',
  hash: '',
  pathPrefix: '/path/prefix',
  search: '',
};

describe('Tests for <BreadcrumbHeader>', () => {

  it("renders breadcrumbs for a pathname", () => {

    loc.pathname = "/namespaces/emojivoto/deployments/web";
    const component = mount(
      <BrowserRouter>
        <BreadcrumbHeader
          location={loc}
          pathPrefix="" />
      </BrowserRouter>
    );

    const crumbs = component.find("span");
    expect(crumbs).toHaveLength(3);
  });
});
