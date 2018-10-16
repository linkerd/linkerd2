import Adapter from 'enzyme-adapter-react-16';
import { BrowserRouter } from 'react-router-dom';
import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import { expect } from 'chai';
import React from 'react';
import Enzyme, { mount } from 'enzyme';

Enzyme.configure({ adapter: new Adapter() });

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

    const crumbs = component.find("a");
    expect(crumbs).to.have.length(3);
  });
});
