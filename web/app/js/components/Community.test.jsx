import Community from './Community.jsx';
import { routerWrap } from '../../test/testHelpers.jsx';
import { mount } from 'enzyme';

describe('Community', () => {
  it('makes a iframe', () => {
    let component = mount(routerWrap(Community));
    expect(component.find("Community")).toHaveLength(1);

    const src = component.find("iframe").props().src;
    expect(src).toEqual("https://linkerd.io/dashboard/");
    
    const title = component.find("iframe").props().title;
    expect(title).toEqual("Community");
  });
});
