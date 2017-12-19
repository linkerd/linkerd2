import { instructions } from './util/Utils.js';
import React from 'react';
import { Col, Row } from 'antd';
import './../../css/cta.css';

export default class CallToAction extends React.Component {
  render() {
    return (
      <div className="call-to-action">
        <div className="action summary">The service mesh was successfully installed!</div>

        <div className="action-steps">
          <Row gutter={0}>
            <Col span={8}>
              <div className="step-container complete">
                <div className="icon-container">
                  <i className="fa fa-check-circle" aria-hidden="true" />
                </div>
                <div className="message"><p>Controller successfully installed</p></div>
              </div>
            </Col>

            <Col span={8}>
              <div className="step-container complete">
                <div className="icon-container">
                  <i className="fa fa-check-circle" aria-hidden="true" />
                </div>
                <div className="message">{this.props.numDeployments || 0} deployments detected</div>
              </div>
            </Col>

            <Col span={8}>
              <div className="step-container incomplete">
                <div className="icon-container">
                  <i className="fa fa-circle-o" aria-hidden="true" />
                </div>
                <div className="message">Connect your first deployment</div>
              </div>
            </Col>
          </Row>
        </div>

        {instructions}
      </div>
    );
  }
}
