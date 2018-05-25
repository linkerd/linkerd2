import _ from 'lodash';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import React from 'react';

export default class AddResourcesMessage extends React.Component {
  render() {
    let unadded = this.props.unadded;
    let resource = this.props.resource;

    if (unadded === 0) {
      return null;
    } else {
      return (
        <div className="add-resources-message">
          {_.isNil(unadded) ? "Some" : unadded} {resource}{unadded === 1 ? " has" : "s have"} not been added to the mesh.
          <div className="clearfix">
            {incompleteMeshMessage()}
          </div>
        </div>
      );
    }
  }
}
