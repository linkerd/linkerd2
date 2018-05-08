import _ from 'lodash';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import React from 'react';
import './../../css/cta.css';

export default class CallToAction extends React.Component {
  render() {
    let resource = this.props.resource || "resource";

    return (
      <div className="call-to-action">
        <div className="action summary">The service mesh was successfully installed!</div>

        <div className="action-steps">
          <div className="step-container complete">
            <div className="icon-container">
              <i className="fa fa-check-circle" aria-hidden="true" />
            </div>
            <div className="message"><p>Controller successfully installed</p></div>
          </div>

          <div className="step-container complete">
            <div className="icon-container">
              <i className="fa fa-check-circle" aria-hidden="true" />
            </div>
            <div className="message">{_.isNil(this.props.numResources) ? "No" : this.props.numResources} {resource}s detected</div>
          </div>

          <div className="step-container incomplete">
            <div className="icon-container">
              <i className="fa fa-circle-o" aria-hidden="true" />
            </div>
            <div className="message">Connect your first {resource}</div>
          </div>
        </div>

        <div className="clearfix">
          {incompleteMeshMessage()}
        </div>
      </div>
    );
  }
}
